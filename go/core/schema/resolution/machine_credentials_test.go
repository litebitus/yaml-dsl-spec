package resolution

import (
	"os"
	"path/filepath"
	"testing"
)

const mockNetrcText = "machine mock.example login mock-user password mock-secret\n" +
	"machine api.mock.example\n  login mock-owner\n  password mock-token\n" +
	"macdef init\n" +
	"default login anyone account mock password any-secret\n"

func TestANetrcGivesAHostItsMachineEntryElseTheDefault(testingContext *testing.T) {
	expectedEntries := map[string][2]string{
		"api.mock.example": {"mock-owner", "mock-token"},
		"other.example":    {"anyone", "any-secret"},
	}
	for hostName, expectedEntry := range expectedEntries {
		loginName, loginPassword, hasEntry := netrcEntry(mockNetrcText, hostName)
		if !hasEntry || [2]string{loginName, loginPassword} != expectedEntry {
			testingContext.Fatalf("%s reads to %s %s", hostName, loginName, loginPassword)
		}
	}
	if _, _, hasEntry := netrcEntry("machine a.example login x password y", "b.example"); hasEntry {
		testingContext.Fatal("another machine's entry was given")
	}
	hostLogin, _ := hostLoginOf(mockNetrcText, "api.mock.example")
	if hostLogin.CredentialOrigin != "the `.netrc` entry for api.mock.example" ||
		hostLogin.RenewalFix != "update the `.netrc` entry for api.mock.example" {
		testingContext.Fatalf("login %+v", hostLogin)
	}
}

func TestALoginIsSentBase64Encoded(testingContext *testing.T) {
	hostLogin := HostLogin{CredentialOrigin: "mock", LoginName: "mock-user", LoginPassword: "mock-secret"}
	if authorization := hostLogin.Authorization(); authorization != "Basic bW9jay11c2VyOm1vY2stc2VjcmV0" {
		testingContext.Fatalf("authorization %s", authorization)
	}
}

func TestTheMachineNetrcIsTheFileNetrcNamesElseTheHomeFolders(testingContext *testing.T) {
	scratchFolder := testingContext.TempDir()
	netrcPath := filepath.Join(scratchFolder, ".netrc")
	if writeError := os.WriteFile(netrcPath, []byte(mockNetrcText), 0o600); writeError != nil {
		testingContext.Fatal(writeError)
	}
	testingContext.Setenv("NETRC", netrcPath)
	if hostLogin, hasLogin := NetrcLogin("mock.example"); !hasLogin || hostLogin.LoginName != "mock-user" {
		testingContext.Fatalf("login %+v", hostLogin)
	}
	credentials := &machineCredentials{}
	if _, hasLogin := credentials.hostLogin("mock.example"); !hasLogin {
		testingContext.Fatal("the named netrc gives no login")
	}
	if removeError := os.Unsetenv("NETRC"); removeError != nil {
		testingContext.Fatal(removeError)
	}
	testingContext.Setenv("HOME", filepath.Join(scratchFolder, "absent"))
	if _, hasLogin := NetrcLogin("mock.example"); hasLogin {
		testingContext.Fatal("a home folder without a netrc gives a login")
	}
	if _, hasLogin := credentials.hostLogin("mock.example"); !hasLogin {
		testingContext.Fatal("the netrc was read again before it was forgotten")
	}
	credentials.forgetCredentials()
	if _, hasLogin := credentials.hostLogin("mock.example"); hasLogin {
		testingContext.Fatal("the forgotten netrc was not read again")
	}
	testingContext.Setenv("HOME", scratchFolder)
	if _, hasLogin := NetrcLogin("mock.example"); !hasLogin {
		testingContext.Fatal("the home folder's netrc gives no login")
	}
}
