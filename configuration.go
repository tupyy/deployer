package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
)

type ConfigurationEntry struct {
	TomcatAddr string `json:"tomcat"`
	Username   string
	Password   string
	Folder     string
	RegexDef   string `json:"regex"`
	Appname    string
	File       string
}

func (c *ConfigurationEntry) Hash() string {
	data := fmt.Sprintf("%s%s%s", c.TomcatAddr, c.Username, c.File)
	h := sha256.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *ConfigurationEntry) compileRegex() *regexp.Regexp {
	r := regexp.MustCompile(c.RegexDef)
	return r
}

type configurations []ConfigurationEntry

func (c *configurations) mapFolderToRegex() map[string][]*regexp.Regexp {
	foldersMap := make(map[string][]*regexp.Regexp)
	for _, entry := range *c {
		regexs := foldersMap[entry.Folder]
		regexs = append(regexs, entry.compileRegex())
		foldersMap[entry.Folder] = regexs
	}

	return foldersMap
}

func (c *configurations) getConfigurationEntry(r *regexp.Regexp) ConfigurationEntry {
	for _, entry := range *c {
		if entry.RegexDef == r.String() {
			return entry
		}
	}

	return ConfigurationEntry{}
}

func (c *configurations) getRegex(folder string) []*regexp.Regexp {
	foldersMap := c.mapFolderToRegex()
	if regex, ok := foldersMap[folder]; ok {
		return regex
	}

	return nil
}
