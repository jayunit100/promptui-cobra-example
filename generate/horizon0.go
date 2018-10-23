/*
Copyright (C) 2018 Synopsys, Inc.
Licensed to the Apache Software Foundation (ASF) under one
or more contributor license agreements. See the NOTICE file
distributed with this work for additional information
regarding copyright ownership. The ASF licenses this file
to you under the Apache License, Version 2.0 (the
"License"); you may not use this file except in compliance
with the License. You may obtain a copy of the License at
http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied. See the License for the
specific language governing permissions and limitations
under the License.
*/

package hub

import (
	"fmt"
	"strings"
	"time"

	horizonapi "github.com/blackducksoftware/horizon/pkg/api"
	"github.com/blackducksoftware/horizon/pkg/components"
	horizon "github.com/blackducksoftware/horizon/pkg/deployer"
	"github.com/blackducksoftware/perceptor-protoform/pkg/api/hub/v1"
	"github.com/blackducksoftware/perceptor-protoform/pkg/util"
	log "github.com/sirupsen/logrus"
)

func (hc *Creater) init(deployer *horizon.Deployer, createHub *v1.HubSpec, hubContainerFlavor *ContainerFlavor, allConfigEnv []*horizonapi.EnvConfig, adminPassword string, userPassword string) error {

	// Create a namespaces
	_, err := util.GetNamespace(hc.KubeClient, createHub.Namespace)
	if err != nil {
		log.Debugf("unable to find the namespace %s", createHub.Namespace)
		deployer.AddNamespace(components.NewNamespace(horizonapi.NamespaceConfig{Name: createHub.Namespace}))
	}

	// Create a secret
	secrets := hc.createHubSecrets(createHub, adminPassword, userPassword)

	for _, secret := range secrets {
		deployer.AddSecret(secret)
	}

	// Create a service account
	serviceAccount := util.CreateServiceAccount(createHub.Namespace, createHub.Namespace)
	deployer.AddServiceAccount(serviceAccount)

	// Create a cluster role binding and associated it to a service account
	clusterRoleBinding := util.CreateClusterRoleBinding(createHub.Namespace, createHub.Namespace, createHub.Namespace, "", "ClusterRole", "cluster-admin")
	deployer.AddClusterRoleBinding(clusterRoleBinding)

	// Create ConfigMaps
	configMaps := hc.createHubConfig(createHub, hubContainerFlavor)

	for _, configMap := range configMaps {
		deployer.AddConfigMap(configMap)
	}

	var storageClass string
	if strings.EqualFold(createHub.PVCStorageClass, "none") {
		storageClass = ""
	} else {
		storageClass = createHub.PVCStorageClass
	}

	if strings.EqualFold(createHub.PVCStorageClass, "none") {
		// Postgres PV
		if strings.EqualFold(createHub.NFSServer, "") {
			return fmt.Errorf("unable to create the PV %s due to missing NFS server path", createHub.Namespace)
		}

		_, err = util.CreatePersistentVolume(hc.KubeClient, createHub.Namespace, storageClass, createHub.PVCClaimSize, hc.Config.NFSPath, createHub.NFSServer)

		if err != nil {
			return fmt.Errorf("unable to create the PV %s due to %+v", createHub.Namespace, err)
		}
	}

	postgresEnvs := allConfigEnv
	postgresEnvs = append(postgresEnvs, &horizonapi.EnvConfig{Type: horizonapi.EnvVal, NameOrPrefix: "POSTGRESQL_USER", KeyOrVal: "blackduck"})
	postgresEnvs = append(postgresEnvs, &horizonapi.EnvConfig{Type: horizonapi.EnvFromSecret, NameOrPrefix: "POSTGRESQL_PASSWORD", KeyOrVal: "HUB_POSTGRES_USER_PASSWORD_FILE", FromName: "db-creds"})
	postgresEnvs = append(postgresEnvs, &horizonapi.EnvConfig{Type: horizonapi.EnvVal, NameOrPrefix: "POSTGRESQL_DATABASE", KeyOrVal: "blackduck"})
	postgresEnvs = append(postgresEnvs, &horizonapi.EnvConfig{Type: horizonapi.EnvFromSecret, NameOrPrefix: "POSTGRESQL_ADMIN_PASSWORD", KeyOrVal: "HUB_POSTGRES_ADMIN_PASSWORD_FILE", FromName: "db-creds"})

	postgresVolumes := []*components.Volume{}
	postgresEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("postgres-persistent-vol")
	postgresInitConfigVol, _ := util.CreateConfigMapVolume("postgres-init-vol", "postgres-init", 0777)
	postgresBootstrapConfigVol, _ := util.CreateConfigMapVolume("postgres-bootstrap-vol", "postgres-bootstrap", 0777)
	postgresVolumes = append(postgresVolumes, postgresEmptyDir, postgresInitConfigVol, postgresBootstrapConfigVol)

	postgresVolumeMounts := []*horizonapi.VolumeMountConfig{}
	postgresVolumeMounts = append(postgresVolumeMounts, &horizonapi.VolumeMountConfig{Name: "postgres-persistent-vol", MountPath: "/var/lib/pgsql/data"})
	postgresVolumeMounts = append(postgresVolumeMounts, &horizonapi.VolumeMountConfig{Name: "postgres-bootstrap-vol:pgbootstrap.sh", MountPath: "/usr/share/container-scripts/postgresql/pgbootstrap.sh"})
	postgresVolumeMounts = append(postgresVolumeMounts, &horizonapi.VolumeMountConfig{Name: "postgres-init-vol:pginit.sh", MountPath: "/usr/share/container-scripts/postgresql/pginit.sh"})

	if strings.EqualFold(createHub.BackupSupport, "Yes") || !strings.EqualFold(createHub.DbPrototype, "empty") {
		// Postgres PVC
		postgresPVC, err := util.CreatePersistentVolumeClaim(createHub.Namespace, createHub.Namespace, createHub.PVCClaimSize, storageClass, horizonapi.ReadWriteOnce)
		if err != nil {
			return fmt.Errorf("failed to create the postgres PVC for %s due to %+v", createHub.Namespace, err)
		}
		deployer.AddPVC(postgresPVC)

		postgresBackupDir, _ := util.CreatePersistentVolumeClaimVolume("postgres-backup-vol", createHub.Namespace)
		postgresVolumes = append(postgresVolumes, postgresBackupDir)
		postgresVolumeMounts = append(postgresVolumeMounts, &horizonapi.VolumeMountConfig{Name: "postgres-backup-vol", MountPath: hc.Config.NFSPath})
	}

	postgresExternalContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "postgres", Image: "registry.access.redhat.com/rhscl/postgresql-96-rhel7:1", PullPolicy: horizonapi.PullAlways,
			MinMem: hubContainerFlavor.PostgresMemoryLimit, MaxMem: "", MinCPU: hubContainerFlavor.PostgresCPULimit, MaxCPU: "", Command: []string{"/usr/share/container-scripts/postgresql/pginit.sh"},
		},
		EnvConfigs:   postgresEnvs,
		VolumeMounts: postgresVolumeMounts,
		PortConfig:   &horizonapi.PortConfig{ContainerPort: postgresPort, Protocol: horizonapi.ProtocolTCP},
		ReadinessProbeConfigs: []*horizonapi.ProbeConfig{{
			ActionConfig:    horizonapi.ActionConfig{Command: []string{"/bin/sh", "-c", `if [[ -f /tmp/BLACKDUCK_MIGRATING ]] ; then cat /tmp/BLACKDUCK_MIGRATING1; fi`}},
			Delay:           1,
			Interval:        5,
			MinCountFailure: 100000,
			Timeout:         100000,
		}},
	}
	initContainers := []*util.Container{}
	// If the PV storage is other than NFS or if the backup is enabled and PV storage is other than NFS, add the init container
	if !strings.EqualFold(createHub.PVCStorageClass, "") && !strings.EqualFold(createHub.PVCStorageClass, "none") {
		postgresInitContainerConfig := &util.Container{
			ContainerConfig: &horizonapi.ContainerConfig{Name: "alpine", Image: "alpine", Command: []string{"sh", "-c", fmt.Sprintf("chmod -cR 777 %s", hc.Config.NFSPath)}},
			VolumeMounts: []*horizonapi.VolumeMountConfig{
				{Name: "postgres-backup-vol", MountPath: hc.Config.NFSPath},
			},
			PortConfig: &horizonapi.PortConfig{ContainerPort: "3001", Protocol: horizonapi.ProtocolTCP},
		}
		initContainers = append(initContainers, postgresInitContainerConfig)
	}

	postgres := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "postgres", Replicas: util.IntToInt32(1)}, "",
		[]*util.Container{postgresExternalContainerConfig}, postgresVolumes, initContainers, []horizonapi.AffinityConfig{})
	deployer.AddReplicationController(postgres)
	deployer.AddService(util.CreateService("postgres", "postgres", createHub.Namespace, postgresPort, postgresPort, horizonapi.ClusterIPServiceTypeDefault))

	// Hub ConfigMap environment variables
	hubConfigEnv := []*horizonapi.EnvConfig{
		{Type: horizonapi.EnvFromConfigMap, FromName: "hub-config"},
	}

	dbSecretVolume := components.NewSecretVolume(horizonapi.ConfigMapOrSecretVolumeConfig{
		VolumeName:      "db-passwords",
		MapOrSecretName: "db-creds",
		Items: map[string]horizonapi.KeyAndMode{
			"HUB_POSTGRES_ADMIN_PASSWORD_FILE": {KeyOrPath: "HUB_POSTGRES_ADMIN_PASSWORD_FILE", Mode: util.IntToInt32(420)},
			"HUB_POSTGRES_USER_PASSWORD_FILE":  {KeyOrPath: "HUB_POSTGRES_USER_PASSWORD_FILE", Mode: util.IntToInt32(420)},
		},
		DefaultMode: util.IntToInt32(420),
	})

	dbEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("cloudsql")

	// cfssl
	// cfsslGCEPersistentDiskVol := CreateGCEPersistentDiskVolume("dir-cfssl", fmt.Sprintf("%s-%s", "cfssl-disk", createHub.Namespace), "ext4")
	cfsslEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("dir-cfssl")
	cfsslContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "cfssl", Image: fmt.Sprintf("%s/%s/%s-cfssl:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "cfssl")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.CfsslMemoryLimit, MaxMem: hubContainerFlavor.CfsslMemoryLimit, MinCPU: "", MaxCPU: ""},
		EnvConfigs:   hubConfigEnv,
		VolumeMounts: []*horizonapi.VolumeMountConfig{{Name: "dir-cfssl", MountPath: "/etc/cfssl"}},
		PortConfig:   &horizonapi.PortConfig{ContainerPort: cfsslPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"/usr/local/bin/docker-healthcheck.sh", "http://localhost:8888/api/v1/cfssl/scaninfo"}},
		// 	Delay:           240,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 10,
		// }},
	}
	cfssl := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "cfssl", Replicas: util.IntToInt32(1)}, "",
		[]*util.Container{cfsslContainerConfig}, []*components.Volume{cfsslEmptyDir}, []*util.Container{},
		[]horizonapi.AffinityConfig{})
	// log.Infof("cfssl : %v\n", cfssl.GetObj())
	deployer.AddReplicationController(cfssl)

	// webserver
	// webServerGCEPersistentDiskVol := CreateGCEPersistentDiskVolume("dir-webserver", fmt.Sprintf("%s-%s", "webserver-disk", createHub.Namespace), "ext4")
	for {
		secret, err := util.GetSecret(hc.KubeClient, createHub.Namespace, "blackduck-certificate")
		if err != nil {
			log.Errorf("unable to get the secret in %s due to %+v", createHub.Namespace, err)
			break
		}
		data := secret.Data
		if len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Second)
	}

	webServerEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("dir-webserver")
	webServerSecretVol, _ := util.CreateSecretVolume("certificate", "blackduck-certificate", 0777)
	webServerContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "webserver", Image: fmt.Sprintf("%s/%s/%s-nginx:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "nginx")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.WebserverMemoryLimit, MaxMem: hubContainerFlavor.WebserverMemoryLimit, MinCPU: "", MaxCPU: "", UID: util.IntToInt64(1000)},
		EnvConfigs: hubConfigEnv,
		VolumeMounts: []*horizonapi.VolumeMountConfig{
			{Name: "dir-webserver", MountPath: "/opt/blackduck/hub/webserver/security"},
			{Name: "certificate", MountPath: "/tmp/secrets"},
		},
		PortConfig: &horizonapi.PortConfig{ContainerPort: webserverPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"/usr/local/bin/docker-healthcheck.sh", "https://localhost:8443/health-checks/liveness", "/tmp/secrets/WEBSERVER_CUSTOM_CERT_FILE"}},
		// 	Delay:           180,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 10,
		// }},
	}
	webserver := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "webserver",
		Replicas: util.IntToInt32(1)}, createHub.Namespace, []*util.Container{webServerContainerConfig}, []*components.Volume{webServerEmptyDir, webServerSecretVol},
		[]*util.Container{}, []horizonapi.AffinityConfig{})
	// log.Infof("webserver : %v\n", webserver.GetObj())
	deployer.AddReplicationController(webserver)

	// documentation
	documentationContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "documentation", Image: fmt.Sprintf("%s/%s/%s-documentation:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "documentation")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.DocumentationMemoryLimit, MaxMem: hubContainerFlavor.DocumentationMemoryLimit, MinCPU: "", MaxCPU: ""},
		EnvConfigs: hubConfigEnv,
		PortConfig: &horizonapi.PortConfig{ContainerPort: documentationPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"/usr/local/bin/docker-healthcheck.sh", "https://127.0.0.1:8443/hubdoc/health-checks/liveness", "/opt/blackduck/hub/hub-documentation/security/root.crt"}},
		// 	Delay:           240,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 10,
		// }},
	}
	documentation := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "documentation", Replicas: util.IntToInt32(1)}, "",
		[]*util.Container{documentationContainerConfig}, []*components.Volume{}, []*util.Container{}, []horizonapi.AffinityConfig{})
	// log.Infof("documentation : %v\n", documentation.GetObj())
	deployer.AddReplicationController(documentation)

	// solr
	// solrGCEPersistentDiskVol := CreateGCEPersistentDiskVolume("dir-solr", fmt.Sprintf("%s-%s", "solr-disk", createHub.Namespace), "ext4")
	solrEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("dir-solr")
	solrContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "solr", Image: fmt.Sprintf("%s/%s/%s-solr:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "solr")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.SolrMemoryLimit, MaxMem: hubContainerFlavor.SolrMemoryLimit, MinCPU: "", MaxCPU: ""},
		EnvConfigs:   hubConfigEnv,
		VolumeMounts: []*horizonapi.VolumeMountConfig{{Name: "dir-solr", MountPath: "/opt/blackduck/hub/solr/cores.data"}},
		PortConfig:   &horizonapi.PortConfig{ContainerPort: solrPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"/usr/local/bin/docker-healthcheck.sh", "http://localhost:8983/solr/project/admin/ping?wt=json"}},
		// 	Delay:           240,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 10,
		// }},
	}
	solr := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "solr", Replicas: util.IntToInt32(1)}, "",
		[]*util.Container{solrContainerConfig}, []*components.Volume{solrEmptyDir}, []*util.Container{},
		[]horizonapi.AffinityConfig{})
	// log.Infof("solr : %v\n", solr.GetObj())
	deployer.AddReplicationController(solr)

	// registration
	// registrationGCEPersistentDiskVol := CreateGCEPersistentDiskVolume("dir-registration", fmt.Sprintf("%s-%s", "registration-disk", createHub.Namespace), "ext4")
	registrationEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("dir-registration")
	registrationContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "registration", Image: fmt.Sprintf("%s/%s/%s-registration:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "registration")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.RegistrationMemoryLimit, MaxMem: hubContainerFlavor.RegistrationMemoryLimit, MinCPU: registrationMinCPUUsage, MaxCPU: ""},
		EnvConfigs:   hubConfigEnv,
		VolumeMounts: []*horizonapi.VolumeMountConfig{{Name: "dir-registration", MountPath: "/opt/blackduck/hub/hub-registration/config"}},
		PortConfig:   &horizonapi.PortConfig{ContainerPort: registrationPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"/usr/local/bin/docker-healthcheck.sh", "https://localhost:8443/registration/health-checks/liveness", "/opt/blackduck/hub/hub-registration/security/root.crt"}},
		// 	Delay:           240,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 10,
		// }},
	}
	registration := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "registration", Replicas: util.IntToInt32(1)}, "",
		[]*util.Container{registrationContainerConfig}, []*components.Volume{registrationEmptyDir}, []*util.Container{},
		[]horizonapi.AffinityConfig{})
	// log.Infof("registration : %v\n", registration.GetObj())
	deployer.AddReplicationController(registration)

	// zookeeper
	zookeeperEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("dir-zookeeper")
	zookeeperContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "zookeeper", Image: fmt.Sprintf("%s/%s/%s-zookeeper:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "zookeeper")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.ZookeeperMemoryLimit, MaxMem: hubContainerFlavor.ZookeeperMemoryLimit, MinCPU: zookeeperMinCPUUsage, MaxCPU: ""},
		EnvConfigs:   hubConfigEnv,
		VolumeMounts: []*horizonapi.VolumeMountConfig{{Name: "dir-zookeeper", MountPath: "/opt/blackduck/hub/logs"}},
		PortConfig:   &horizonapi.PortConfig{ContainerPort: zookeeperPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"zkServer.sh", "status", "/opt/blackduck/zookeeper/conf/zoo.cfg"}},
		// 	Delay:           240,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 10,
		// }},
	}
	zookeeper := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "zookeeper", Replicas: util.IntToInt32(1)}, "",
		[]*util.Container{zookeeperContainerConfig}, []*components.Volume{zookeeperEmptyDir}, []*util.Container{}, []horizonapi.AffinityConfig{})
	// log.Infof("zookeeper : %v\n", zookeeper.GetObj())
	deployer.AddReplicationController(zookeeper)

	// jobRunner
	jobRunnerEnvs := allConfigEnv
	jobRunnerEnvs = append(jobRunnerEnvs, &horizonapi.EnvConfig{Type: horizonapi.EnvFromConfigMap, NameOrPrefix: "HUB_MAX_MEMORY", KeyOrVal: "jobrunner-mem", FromName: "hub-config-resources"})
	jobRunnerContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "jobrunner", Image: fmt.Sprintf("%s/%s/%s-jobrunner:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "jobrunner")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.JobRunnerMemoryLimit, MaxMem: hubContainerFlavor.JobRunnerMemoryLimit, MinCPU: jonRunnerMinCPUUsage, MaxCPU: jonRunnerMaxCPUUsage},
		EnvConfigs:   jobRunnerEnvs,
		VolumeMounts: []*horizonapi.VolumeMountConfig{{Name: "db-passwords", MountPath: "/tmp/secrets"}},
		PortConfig:   &horizonapi.PortConfig{ContainerPort: jobRunnerPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"/usr/local/bin/docker-healthcheck.sh"}},
		// 	Delay:           240,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 10,
		// }},
	}

	jobRunner := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "jobrunner", Replicas: hubContainerFlavor.JobRunnerReplicas}, "",
		[]*util.Container{jobRunnerContainerConfig}, []*components.Volume{dbSecretVolume, dbEmptyDir}, []*util.Container{},
		[]horizonapi.AffinityConfig{})
	// log.Infof("jobRunner : %v\n", jobRunner.GetObj())
	deployer.AddReplicationController(jobRunner)

	// hub-scan
	scannerEnvs := allConfigEnv
	scannerEnvs = append(scannerEnvs, &horizonapi.EnvConfig{Type: horizonapi.EnvFromConfigMap, NameOrPrefix: "HUB_MAX_MEMORY", KeyOrVal: "scan-mem", FromName: "hub-config-resources"})
	hubScanEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("dir-scan")
	hubScanContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "hub-scan", Image: fmt.Sprintf("%s/%s/%s-scan:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "scan")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.ScanMemoryLimit, MaxMem: hubContainerFlavor.ScanMemoryLimit, MinCPU: "", MaxCPU: ""},
		EnvConfigs: scannerEnvs,
		VolumeMounts: []*horizonapi.VolumeMountConfig{
			{Name: "db-passwords", MountPath: "/tmp/secrets"},
			{Name: "dir-scan", MountPath: "/opt/blackduck/hub/hub-scan/security"}},
		PortConfig: &horizonapi.PortConfig{ContainerPort: scannerPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"/usr/local/bin/docker-healthcheck.sh", "https://127.0.0.1:8443/api/health-checks/liveness", "/opt/blackduck/hub/hub-scan/security/root.crt"}},
		// 	Delay:           240,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 10,
		// }},
	}
	hubScan := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "hub-scan", Replicas: hubContainerFlavor.ScanReplicas}, "",
		[]*util.Container{hubScanContainerConfig}, []*components.Volume{hubScanEmptyDir, dbSecretVolume, dbEmptyDir}, []*util.Container{}, []horizonapi.AffinityConfig{})
	// log.Infof("hubScan : %v\n", hubScan.GetObj())
	deployer.AddReplicationController(hubScan)

	// hub-authentication
	authEnvs := allConfigEnv
	authEnvs = append(authEnvs, &horizonapi.EnvConfig{Type: horizonapi.EnvVal, NameOrPrefix: "HUB_MAX_MEMORY", KeyOrVal: hubContainerFlavor.AuthenticationHubMaxMemory})
	// hubAuthGCEPersistentDiskVol := CreateGCEPersistentDiskVolume("dir-authentication", fmt.Sprintf("%s-%s", "authentication-disk", createHub.Namespace), "ext4")
	hubAuthEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("dir-authentication")
	hubAuthContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "hub-authentication", Image: fmt.Sprintf("%s/%s/%s-authentication:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "authentication")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.AuthenticationMemoryLimit, MaxMem: hubContainerFlavor.AuthenticationMemoryLimit, MinCPU: "", MaxCPU: ""},
		EnvConfigs: authEnvs,
		VolumeMounts: []*horizonapi.VolumeMountConfig{
			{Name: "db-passwords", MountPath: "/tmp/secrets"},
			{Name: "dir-authentication", MountPath: "/opt/blackduck/hub/hub-authentication/security"}},
		PortConfig: &horizonapi.PortConfig{ContainerPort: authenticationPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"/usr/local/bin/docker-healthcheck.sh", "https://127.0.0.1:8443/api/health-checks/liveness", "/opt/blackduck/hub/hub-authentication/security/root.crt"}},
		// 	Delay:           240,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 10,
		// }},
	}
	hubAuth := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "hub-authentication", Replicas: util.IntToInt32(1)}, "",
		[]*util.Container{hubAuthContainerConfig}, []*components.Volume{hubAuthEmptyDir, dbSecretVolume, dbEmptyDir}, []*util.Container{},
		[]horizonapi.AffinityConfig{})
	// log.Infof("hubAuth : %v\n", hubAuthc.GetObj())
	deployer.AddReplicationController(hubAuth)

	// webapp-logstash
	webappEnvs := allConfigEnv
	webappEnvs = append(webappEnvs, &horizonapi.EnvConfig{Type: horizonapi.EnvFromConfigMap, NameOrPrefix: "HUB_MAX_MEMORY", KeyOrVal: "webapp-mem", FromName: "hub-config-resources"})
	// webappGCEPersistentDiskVol := CreateGCEPersistentDiskVolume("dir-webapp", fmt.Sprintf("%s-%s", "webapp-disk", createHub.Namespace), "ext4")
	webappEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("dir-webapp")
	webappContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "webapp", Image: fmt.Sprintf("%s/%s/%s-webapp:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "webapp")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.WebappMemoryLimit, MaxMem: hubContainerFlavor.WebappMemoryLimit, MinCPU: hubContainerFlavor.WebappCPULimit,
			MaxCPU: hubContainerFlavor.WebappCPULimit},
		EnvConfigs: webappEnvs,
		VolumeMounts: []*horizonapi.VolumeMountConfig{
			{Name: "db-passwords", MountPath: "/tmp/secrets"},
			{Name: "dir-webapp", MountPath: "/opt/blackduck/hub/hub-webapp/security"},
			{Name: "dir-logstash", MountPath: "/opt/blackduck/hub/logs"}},
		PortConfig: &horizonapi.PortConfig{ContainerPort: webappPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"/usr/local/bin/docker-healthcheck.sh", "https://127.0.0.1:8443/api/health-checks/liveness", "/opt/blackduck/hub/hub-webapp/security/root.crt"}},
		// 	Delay:           360,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 1000,
		// }},
	}
	logstashEmptyDir, _ := util.CreateEmptyDirVolumeWithoutSizeLimit("dir-logstash")
	logstashContainerConfig := &util.Container{
		ContainerConfig: &horizonapi.ContainerConfig{Name: "logstash", Image: fmt.Sprintf("%s/%s/%s-logstash:%s", createHub.DockerRegistry, createHub.DockerRepo, createHub.ImagePrefix, hc.getTag(createHub, "logstash")),
			PullPolicy: horizonapi.PullAlways, MinMem: hubContainerFlavor.LogstashMemoryLimit, MaxMem: hubContainerFlavor.LogstashMemoryLimit, MinCPU: "", MaxCPU: ""},
		EnvConfigs:   hubConfigEnv,
		VolumeMounts: []*horizonapi.VolumeMountConfig{{Name: "dir-logstash", MountPath: "/var/lib/logstash/data"}},
		PortConfig:   &horizonapi.PortConfig{ContainerPort: logstashPort, Protocol: horizonapi.ProtocolTCP},
		// LivenessProbeConfigs: []*horizonapi.ProbeConfig{{
		// 	ActionConfig:    horizonapi.ActionConfig{Command: []string{"/usr/local/bin/docker-healthcheck.sh", "http://localhost:9600/"}},
		// 	Delay:           240,
		// 	Interval:        30,
		// 	Timeout:         10,
		// 	MinCountFailure: 1000,
		// }},
	}
	webappLogstash := util.CreateReplicationControllerFromContainer(&horizonapi.ReplicationControllerConfig{Namespace: createHub.Namespace, Name: "webapp-logstash", Replicas: util.IntToInt32(1)},
		"", []*util.Container{webappContainerConfig, logstashContainerConfig}, []*components.Volume{webappEmptyDir, logstashEmptyDir, dbSecretVolume, dbEmptyDir},
		[]*util.Container{}, []horizonapi.AffinityConfig{})
	// log.Infof("webappLogstash : %v\n", webappLogstashc.GetObj())
	deployer.AddReplicationController(webappLogstash)

	deployer.AddService(util.CreateService("cfssl", "cfssl", createHub.Namespace, cfsslPort, cfsslPort, horizonapi.ClusterIPServiceTypeDefault))
	deployer.AddService(util.CreateService("zookeeper", "zookeeper", createHub.Namespace, zookeeperPort, zookeeperPort, horizonapi.ClusterIPServiceTypeDefault))
	deployer.AddService(util.CreateService("webserver", "webserver", createHub.Namespace, "443", webserverPort, horizonapi.ClusterIPServiceTypeDefault))
	deployer.AddService(util.CreateService("webserver-np", "webserver", createHub.Namespace, "443", webserverPort, horizonapi.ClusterIPServiceTypeNodePort))
	deployer.AddService(util.CreateService("webserver-lb", "webserver", createHub.Namespace, "443", webserverPort, horizonapi.ClusterIPServiceTypeLoadBalancer))
	deployer.AddService(util.CreateService("webapp", "webapp-logstash", createHub.Namespace, webappPort, webappPort, horizonapi.ClusterIPServiceTypeDefault))
	deployer.AddService(util.CreateService("logstash", "webapp-logstash", createHub.Namespace, logstashPort, logstashPort, horizonapi.ClusterIPServiceTypeDefault))
	deployer.AddService(util.CreateService("solr", "solr", createHub.Namespace, solrPort, solrPort, horizonapi.ClusterIPServiceTypeDefault))
	deployer.AddService(util.CreateService("documentation", "documentation", createHub.Namespace, documentationPort, documentationPort, horizonapi.ClusterIPServiceTypeDefault))
	deployer.AddService(util.CreateService("scan", "hub-scan", createHub.Namespace, scannerPort, scannerPort, horizonapi.ClusterIPServiceTypeDefault))
	deployer.AddService(util.CreateService("authentication", "hub-authentication", createHub.Namespace, authenticationPort, authenticationPort, horizonapi.ClusterIPServiceTypeDefault))
	deployer.AddService(util.CreateService("registration", "registration", createHub.Namespace, registrationPort, registrationPort, horizonapi.ClusterIPServiceTypeDefault))

}
