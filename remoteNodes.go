package main

import (
	"strconv"

	log "github.com/sirupsen/logrus"
)

type nodes struct {
	Nodes []node `json:"nodes"`
}

type node struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Port uint64 `json:"port"`
	SSL  bool   `json:"ssl"`
}

const urlBitcoinNovaRemoteNodes = "https://raw.githubusercontent.com/BitcoinNova/bitcoinnova-nodes-json/master/Bitcoinnova-nodes.json"

func requestListRemoteNodes() (remoteNodes []node) {

	theNodes := new(nodes)
	err := getJSONFromHTTPRequest(urlBitcoinNovaRemoteNodes, theNodes)

	if err != nil {

		// if error getting the list, include only one node - the default one
		var defaultNode node
		defaultNode.Name = defaultRemoteDaemonName
		defaultNode.URL = defaultRemoteDaemonAddress
		defaultPort, err := strconv.ParseUint(defaultRemoteDaemonPort, 10, 64)
		if err != nil {
			log.Fatal("error parsing remote node port: ", err)
		}
		defaultNode.Port = defaultPort
		defaultNode.SSL = defaultRemoteDaemonSSL

		remoteNodes = append(remoteNodes, defaultNode)

	} else {

		remoteNodes = theNodes.Nodes
	}

	// add an item for displaying the custom node in the dropdown list
	var customNode node
	customNode.Name = "Custom (change in settings)"
	customNode.URL = "Custom (change in settings)"

	remoteNodes = append(remoteNodes, customNode)

	return remoteNodes
}
