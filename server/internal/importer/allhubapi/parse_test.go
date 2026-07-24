package allhubapi

import (
	"errors"
	"strings"
	"testing"
)

func TestParseV2NestedAccounts(t *testing.T) {
	backup, err := Parse([]byte(`{"version":"2.0","timestamp":1710000000000,"accounts":{"accounts":[{"site_name":"New API","site_url":"https://example.com/","site_type":"new-api","account_info":{"id":42,"access_token":"secret"},"checkIn":{"enableDetection":true,"autoCheckInEnabled":true}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if backup.Version != "2.0" || len(backup.Accounts) != 1 {
		t.Fatalf("backup = %+v", backup)
	}
	account := backup.Accounts[0]
	if account.UserID != "42" || account.AccessToken != "secret" || !account.CheckinEnabled {
		t.Fatalf("account = %+v", account)
	}
}

func TestParseLegacyDataAccountsAndCookieMarker(t *testing.T) {
	backup, err := Parse([]byte(`{"timestamp":"2026-07-23T00:00:00Z","data":{"accounts":[{"site_name":"Legacy","site_url":"https://legacy.example","site_type":"new-api","authType":"cookie","account_info":{"id":"u-1","access_token":"token"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(backup.Accounts) != 1 || backup.Accounts[0].AuthType != "cookie" {
		t.Fatalf("accounts = %+v", backup.Accounts)
	}
}

func TestParseRejectsUnsupportedVersionAndMalformedRecords(t *testing.T) {
	_, err := Parse([]byte(`{"version":"3.0","timestamp":1,"accounts":[]}`))
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("error = %v", err)
	}
	backup, err := Parse([]byte(`{"version":"2.0","timestamp":1,"accounts":[{"site_name":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if backup.Accounts[0].ParseError == "" || strings.Contains(backup.Accounts[0].ParseError, "token") {
		t.Fatalf("account = %+v", backup.Accounts[0])
	}
}

func TestParseRejectsOversizedAccountList(t *testing.T) {
	accounts := strings.Repeat(`{"site_name":"x"},`, MaxAccounts)
	payload := `{"version":"2.0","timestamp":1,"accounts":[` + accounts + `{"site_name":"x"}]}`
	if _, err := Parse([]byte(payload)); err == nil {
		t.Fatal("oversized account list accepted")
	}
}
