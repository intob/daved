#!/bin/bash
while IFS= read -r host
do
    echo "UPDATING HOST $host"
    ssh -n -o StrictHostKeyChecking=no admin@$host "sudo rm daved daved_edge.service.conf"
    scp -o StrictHostKeyChecking=no daved daved_edge.service.conf admin@$host:~
    ssh -n -o StrictHostKeyChecking=no admin@$host "sudo cp -f daved_edge.service.conf /etc/systemd/system/daved.service && sudo systemctl daemon-reload && sudo systemctl enable daved && sudo systemctl restart daved && sudo systemctl status daved"
    sleep 2 # So they don't start in lock-step.
done < "hostnames_edges"
