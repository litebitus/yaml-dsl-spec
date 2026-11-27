package resolution

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// HostLogin is a host's login, where it came from, and how its owner renews it.
type HostLogin struct {
	// CredentialOrigin is where the login came from, as a report names it.
	CredentialOrigin string
	LoginName        string
	LoginPassword    string
	// RenewalFix is what renews the login.
	RenewalFix string
}

// Authorization is the `Authorization` header value the login is sent as.
func (hostLogin HostLogin) Authorization() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(hostLogin.LoginName+":"+hostLogin.LoginPassword))
}

// credentialSource is where a link's credentials come from.
type credentialSource interface {
	// hostLogin is the login `.netrc` gives hostName, and whether it gives one.
	hostLogin(hostName string) (HostLogin, bool)
	// forgetCredentials reads the credentials again when next asked, after every one was refused.
	forgetCredentials()
}

// machineCredentials is the machine's `.netrc`, read when first needed.
type machineCredentials struct {
	netrcLock   sync.Mutex
	isNetrcRead bool
	netrcText   string
	hasNetrc    bool
}

func (credentials *machineCredentials) hostLogin(hostName string) (HostLogin, bool) {
	credentials.netrcLock.Lock()
	defer credentials.netrcLock.Unlock()
	if !credentials.isNetrcRead {
		credentials.netrcText, credentials.hasNetrc = machineNetrcText()
		credentials.isNetrcRead = true
	}
	if !credentials.hasNetrc {
		return HostLogin{}, false
	}
	return hostLoginOf(credentials.netrcText, hostName)
}

func (credentials *machineCredentials) forgetCredentials() {
	credentials.netrcLock.Lock()
	defer credentials.netrcLock.Unlock()
	credentials.isNetrcRead = false
}

// NetrcLogin is the login the machine's `.netrc` gives hostName, its `machine` entry else its `default` entry, and
// whether it gives one: the file `NETRC` names, else the one in the user's home folder.
func NetrcLogin(hostName string) (HostLogin, bool) {
	netrcText, hasNetrc := machineNetrcText()
	if !hasNetrc {
		return HostLogin{}, false
	}
	return hostLoginOf(netrcText, hostName)
}

func machineNetrcText() (string, bool) {
	netrcPath, isNamed := os.LookupEnv("NETRC")
	if !isNamed {
		homeFolder, hasHome := os.LookupEnv("HOME")
		if !hasHome {
			return "", false
		}
		netrcPath = filepath.Join(homeFolder, ".netrc")
	}
	netrcBytes, readError := os.ReadFile(netrcPath)
	if readError != nil {
		return "", false
	}
	return string(netrcBytes), true
}

func hostLoginOf(netrcText, hostName string) (HostLogin, bool) {
	loginName, loginPassword, hasEntry := netrcEntry(netrcText, hostName)
	if !hasEntry {
		return HostLogin{}, false
	}
	return HostLogin{
		CredentialOrigin: "the `.netrc` entry for " + hostName,
		LoginName:        loginName,
		LoginPassword:    loginPassword,
		RenewalFix:       "update the `.netrc` entry for " + hostName,
	}, true
}

// netrcLoginEntry is a `.netrc` entry's login and password.
type netrcLoginEntry struct {
	loginName     string
	loginPassword string
}

// netrcEntry is the login and password a `.netrc`'s text gives hostName: its `machine` entry, else its `default`
// entry.
func netrcEntry(netrcText, hostName string) (string, string, bool) {
	netrcWords := strings.Fields(netrcText)
	var hostEntry, defaultEntry *netrcLoginEntry
	var readingEntry netrcLoginEntry
	isInEntry, isHostEntry, isDefaultEntry := false, false, false
	finishEntry := func() {
		if isInEntry {
			takenEntry := readingEntry
			readingEntry = netrcLoginEntry{}
			if isHostEntry && hostEntry == nil {
				hostEntry = &takenEntry
			}
			if isDefaultEntry && defaultEntry == nil {
				defaultEntry = &takenEntry
			}
		}
	}
	nextWord := func(wordIndex *int) (string, bool) {
		if *wordIndex+1 >= len(netrcWords) {
			*wordIndex = len(netrcWords)
			return "", false
		}
		*wordIndex++
		return netrcWords[*wordIndex], true
	}
	for wordIndex := 0; wordIndex < len(netrcWords); wordIndex++ {
		switch netrcWords[wordIndex] {
		case "machine":
			finishEntry()
			machineName, hasName := nextWord(&wordIndex)
			isInEntry, isHostEntry, isDefaultEntry = true, hasName && machineName == hostName, false
		case "default":
			finishEntry()
			isInEntry, isHostEntry, isDefaultEntry = true, false, true
		case "login":
			readingEntry.loginName, _ = nextWord(&wordIndex)
		case "password":
			readingEntry.loginPassword, _ = nextWord(&wordIndex)
		case "account":
			nextWord(&wordIndex)
		case "macdef":
			finishEntry()
			isInEntry = false
			nextWord(&wordIndex)
		}
	}
	finishEntry()
	if hostEntry != nil {
		return hostEntry.loginName, hostEntry.loginPassword, true
	}
	if defaultEntry != nil {
		return defaultEntry.loginName, defaultEntry.loginPassword, true
	}
	return "", "", false
}
