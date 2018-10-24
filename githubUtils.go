// Copyright (c) 2018, The Bitcoin Nova Developers
//
// Please see the included LICENSE file for more information.
//

package main

import (
	"github.com/mcuadros/go-version"
)

type release struct {
	TagName string `json:"tag_name"`
	URL     string `json:"html_url"`
}

const url = "https://api.github.com/repos/Bitcoin-N/BitcoinNova-Nest/releases/latest"

func checkIfNewReleaseAvailableOnGithub(currentVersion string) (newVersion string, urlNewVersion string) {

	latestRelease := new(release)
	getJSONFromHTTPRequest(url, latestRelease)

	if version.CompareSimple(latestRelease.TagName, currentVersion) == 1 {
		return latestRelease.TagName, latestRelease.URL
	}

	return "", ""
}
