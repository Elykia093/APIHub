package allhubapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

const MaxAccounts = 100

var (
	ErrInvalidFormat      = errors.New("invalid all-api-hub backup format")
	ErrUnsupportedVersion = errors.New("unsupported all-api-hub backup version")
	ErrNoAccounts         = errors.New("all-api-hub backup contains no accounts")
)

type Backup struct {
	Version  string
	Accounts []Account
}

type Account struct {
	Index          int
	Name           string
	BaseURL        string
	SiteType       string
	UserID         string
	AccessToken    string
	AuthType       string
	Disabled       bool
	CheckinEnabled bool
	ParseError     string
}

func Parse(payload []byte) (Backup, error) {
	var root map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&root); err != nil || root == nil {
		return Backup{}, ErrInvalidFormat
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return Backup{}, ErrInvalidFormat
	}
	if !validTimestamp(root["timestamp"]) {
		return Backup{}, ErrInvalidFormat
	}

	version := "1.0"
	if raw, ok := root["version"]; ok {
		value, err := stringField(raw)
		if err != nil || strings.TrimSpace(value) == "" {
			return Backup{}, ErrInvalidFormat
		}
		version = strings.TrimSpace(value)
	}
	if version != "1.0" && version != "2.0" {
		return Backup{}, ErrUnsupportedVersion
	}

	accounts, ok := extractAccountValues(root)
	if !ok {
		return Backup{}, ErrNoAccounts
	}
	if len(accounts) > MaxAccounts {
		return Backup{}, fmt.Errorf("backup contains more than %d accounts", MaxAccounts)
	}
	parsed := make([]Account, 0, len(accounts))
	for index, raw := range accounts {
		parsed = append(parsed, parseAccount(index, raw))
	}
	return Backup{Version: version, Accounts: parsed}, nil
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return ErrInvalidFormat
}

func validTimestamp(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number > 0 && !math.IsNaN(number) && !math.IsInf(number, 0)
	}
	value, err := stringField(raw)
	return err == nil && strings.TrimSpace(value) != ""
}

func extractAccountValues(root map[string]json.RawMessage) ([]json.RawMessage, bool) {
	if values, ok := accountArray(root["accounts"]); ok {
		return values, true
	}
	var data map[string]json.RawMessage
	if raw, ok := root["data"]; ok && json.Unmarshal(raw, &data) == nil {
		if values, ok := accountArray(data["accounts"]); ok {
			return values, true
		}
	}
	return nil, false
}

func accountArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) == nil {
		return values, true
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil, false
	}
	values, ok := objectArray(object["accounts"])
	return values, ok
}

func objectArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false
	}
	return values, true
}

func parseAccount(index int, raw json.RawMessage) Account {
	account := Account{Index: index, AuthType: "access_token"}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		account.ParseError = "account must be an object"
		return account
	}

	var err error
	account.Name, err = firstString(object, "site_name", "siteName")
	if err != nil {
		account.ParseError = "site name must be a string"
		return account
	}
	account.BaseURL, err = firstString(object, "site_url", "siteUrl")
	if err != nil {
		account.ParseError = "site URL must be a string"
		return account
	}
	account.SiteType, err = firstString(object, "site_type", "siteType")
	if err != nil {
		account.ParseError = "site type must be a string"
		return account
	}
	if value, ok, fieldErr := firstOptionalBool(object, "disabled"); fieldErr != nil {
		account.ParseError = "disabled must be a boolean"
		return account
	} else if ok {
		account.Disabled = value
	}
	if value, ok, fieldErr := firstOptionalString(object, "authType", "auth_type"); fieldErr != nil {
		account.ParseError = "auth type must be a string"
		return account
	} else if ok {
		account.AuthType = strings.TrimSpace(value)
	}

	info, infoErr := firstObject(object, "account_info", "accountInfo")
	if infoErr != nil {
		account.ParseError = "account info must be an object"
		return account
	}
	if info == nil {
		info = object
	}
	account.UserID, err = firstIdentity(info, "id", "userId", "user_id")
	if err != nil {
		account.ParseError = "account id must be a string or number"
		return account
	}
	account.AccessToken, err = firstString(info, "access_token", "accessToken")
	if err != nil {
		account.ParseError = "access token must be a string"
		return account
	}

	checkIn, checkInErr := firstObject(object, "checkIn", "check_in")
	if checkInErr != nil {
		account.ParseError = "check-in configuration must be an object"
		return account
	}
	if checkIn != nil {
		enabled, enabledOK, fieldErr := firstOptionalBool(checkIn, "enableDetection", "enable_detection")
		if fieldErr != nil {
			account.ParseError = "check-in enableDetection must be a boolean"
			return account
		}
		auto, autoOK, fieldErr := firstOptionalBool(checkIn, "autoCheckInEnabled", "auto_check_in_enabled")
		if fieldErr != nil {
			account.ParseError = "check-in autoCheckInEnabled must be a boolean"
			return account
		}
		account.CheckinEnabled = enabledOK && enabled && (!autoOK || auto)
	}
	return account
}

func firstString(object map[string]json.RawMessage, keys ...string) (string, error) {
	value, ok, err := firstOptionalString(object, keys...)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	return strings.TrimSpace(value), nil
}

func firstOptionalString(object map[string]json.RawMessage, keys ...string) (string, bool, error) {
	for _, key := range keys {
		raw, ok := object[key]
		if !ok {
			continue
		}
		value, err := stringField(raw)
		return value, true, err
	}
	return "", false, nil
}

func stringField(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}

func firstIdentity(object map[string]json.RawMessage, keys ...string) (string, error) {
	for _, key := range keys {
		raw, ok := object[key]
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil {
			return strings.TrimSpace(value), nil
		}
		var number json.Number
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&number) == nil {
			if _, err := strconv.ParseFloat(string(number), 64); err == nil {
				return strings.TrimSpace(string(number)), nil
			}
		}
		return "", ErrInvalidFormat
	}
	return "", nil
}

func firstOptionalBool(object map[string]json.RawMessage, keys ...string) (bool, bool, error) {
	for _, key := range keys {
		raw, ok := object[key]
		if !ok {
			continue
		}
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return false, true, err
		}
		return value, true, nil
	}
	return false, false, nil
}

func firstObject(object map[string]json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
	for _, key := range keys {
		raw, ok := object[key]
		if !ok {
			continue
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	}
	return nil, nil
}
