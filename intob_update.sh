#!/bin/bash
export HOST=$1
export SSHUSER=$2
export CONF=$3
export OPT="StrictHostKeyChecking=no"
echo "UPDATING HOST $HOST WITH SSHUSER:$SSHUSER CONF:$CONF"
#ssh -o $OPT -n $SSHUSER@$HOST "sudo rm daved $CONF" # scp overwrites by default
scp -o $OPT daved $CONF $SSHUSER@$HOST:~
ssh -o $OPT -n $SSHUSER@$HOST "sudo cp -f $CONF /etc/systemd/system/daved.service && sudo cp -f daved /root/daved && sudo systemctl daemon-reload && sudo systemctl enable daved && sudo systemctl restart daved && sudo systemctl status daved"

