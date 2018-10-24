#!/bin/bash
echo "building the blackduckctl client"
go build -o blackduckctl ./cmd
ls -altrh | grep ctl
