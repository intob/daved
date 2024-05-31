#!/bin/bash
export HOST=$1
echo "UPDATING HOST $HOST"
ssh -i ~/.ssh/awsintob.pem -n -o StrictHostKeyChecking=no admin@$HOST "sudo rm daved daved_edge.service.conf"
scp -i ~/.ssh/awsintob.pem -o StrictHostKeyChecking=no daved intob_daved_edge.service.conf admin@$HOST:~
ssh -i ~/.ssh/awsintob.pem -n -o StrictHostKeyChecking=no admin@$HOST "sudo cp -f intob_daved_edge.service.conf /etc/systemd/system/daved.service && sudo systemctl daemon-reload && sudo systemctl enable daved && sudo systemctl restart daved && sudo systemctl status daved"

