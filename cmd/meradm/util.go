package main

import "regexp"

// Simple regex to ensure we have ip:port. We rely on merlin to perform proper validation.
var ipPortRegex = regexp.MustCompile(`^([\d.]+):(\d+)$`)

// Simple regex to ensure we have an ip. We rely on merlin to perform proper validation.
var ipRegex = regexp.MustCompile(`^[\d.]+$`)
