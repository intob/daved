git pull
sudo cp -f daved_edge.service.conf /etc/systemd/system/daved.service
/usr/local/go/bin/go build
systemctl daemon-reload
systemctl enable daved.service
systemctl stop daved.service
systemctl start daved.service
