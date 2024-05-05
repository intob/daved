#!/bin/bash
GO_VERSION=$1
ARCH=$2
apt-get update && apt-get -y install git
wget https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf go${GO_VERSION}.linux-${ARCH}.tar.gz
rm go${GO_VERSION}.linux-${ARCH}.tar.gz
echo "GOROOT=/usr/local/go" >> ~/.profile
echo "GOPATH=\$HOME/go" >> ~/.profile
echo "PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin" >> ~/.profile
echo "GOROOT=/usr/local/go" >> /etc/profile
echo "GOPATH=\$HOME/go" >> /etc/profile
echo "PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin" >> /etc/profile
source ~/.profile
go version
