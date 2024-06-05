#!/bin/bash
./intob_update.sh admin edge1 intob_edge.service.conf
./intob_update.sh joey pi1.local intob_peer.service.conf
./intob_update.sh admin aws1 intob_peer.service.conf
./intob_update.sh admin aws2 intob_peer.service.conf
./intob_update.sh admin aws3 intob_peer.service.conf
./intob_update.sh admin aws4 intob_peer.service.conf
./intob_update.sh admin aws5 intob_peer.service.conf
./intob_update.sh admin aws6 intob_peer.service.conf
