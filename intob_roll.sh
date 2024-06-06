#!/bin/bash
./intob_update.sh admin edge1 intob_edge.service.conf
./intob_update.sh joey pi1.local intob_pi1.service.conf
./intob_update.sh joey pi2.local intob_pi2.service.conf
./intob_update.sh joey pi3.local intob_pi3.service.conf
./intob_update.sh joey pi4.local intob_pi4.service.conf
./intob_update.sh admin aws1 intob_peer.service.conf
./intob_update.sh admin aws2 intob_peer.service.conf
