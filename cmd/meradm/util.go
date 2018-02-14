package main

import "regexp"

var hostPortRegex = regexp.MustCompile(`^([^:]+):(\d+)$`)
