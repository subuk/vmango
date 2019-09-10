#!/bin/sh

echo ARGS: $@
echo "VM ${VMANGO_VM_ID} has been created with root volume ${VMANGO_VM_VOLUME_0_PATH}"
echo "Env:"
env | grep VMANGO_
