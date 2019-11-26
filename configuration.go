package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type ConfigurationEntry struct {
	TomcatAddr string `json:"tomcat"`
	Username   string
	Password   string
	Folder     string
	Regex      string
	File       string
}

func (c *ConfigurationEntry) Hash() string {
	data := fmt.Sprintf("%s%s%s", c.TomcatAddr, c.Username, c.File)
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}
