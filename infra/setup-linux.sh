#!/bin/sh
# Moved. Host provisioning now lives in provision/ as versioned config files
# plus an idempotent apply.sh, instead of one big script.
echo "setup-linux.sh has been replaced by provision/apply.sh"
echo "run:  NC_PUBLIC_IP=<ip> provision/apply.sh   (see provision/README.md)"
exit 1
