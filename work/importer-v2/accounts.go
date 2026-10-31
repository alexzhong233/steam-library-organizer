package main

import (
	"fmt"
	"strconv"
	"strings"
	"text/scanner"
)

const steamIDBase uint64 = 76561197960265728

func accountFromSteamID(raw string) (string, error) {
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || n <= steamIDBase || n-steamIDBase > 4294967295 {
		return "", fmt.Errorf("Steam 登录账号无效")
	}
	return strconv.FormatUint(n-steamIDBase, 10), nil
}

// loginusers.vdf varies between Steam versions: macOS may write AutoLogin
// instead of MostRecent. Parse KeyValues so whitespace and key casing do not
// determine whether a signed-in user can be identified.
func accountFromLoginUsers(data string) (string, error) {
	var lexer scanner.Scanner
	lexer.Init(strings.NewReader(data))
	lexer.Mode = scanner.ScanStrings | scanner.ScanComments | scanner.SkipComments
	var scanErr error
	lexer.Error = func(_ *scanner.Scanner, msg string) { scanErr = fmt.Errorf("Steam 登录文件格式无效：%s", msg) }
	var object func(bool) (map[string]any, error)
	object = func(nested bool) (map[string]any, error) {
		out := map[string]any{}
		for {
			tok := lexer.Scan()
			if tok == scanner.EOF {
				if nested {
					return nil, fmt.Errorf("Steam 登录文件未结束")
				}
				return out, scanErr
			}
			if tok == '}' && nested {
				return out, scanErr
			}
			if tok != scanner.String {
				return nil, fmt.Errorf("Steam 登录文件键无效")
			}
			key, err := strconv.Unquote(lexer.TokenText())
			if err != nil {
				return nil, err
			}
			key = strings.ToLower(key)
			if _, exists := out[key]; exists {
				return nil, fmt.Errorf("Steam 登录文件键重复")
			}
			tok = lexer.Scan()
			if tok == '{' {
				value, err := object(true)
				if err != nil {
					return nil, err
				}
				out[key] = value
			} else if tok == scanner.String {
				value, err := strconv.Unquote(lexer.TokenText())
				if err != nil {
					return nil, err
				}
				out[key] = value
			} else {
				return nil, fmt.Errorf("Steam 登录文件值无效")
			}
		}
	}
	root, err := object(false)
	if err != nil {
		return "", err
	}
	users, ok := root["users"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("Steam 登录文件没有账号列表")
	}
	all, recent, auto := []string{}, []string{}, []string{}
	for id, raw := range users {
		fields, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("Steam 登录账号记录无效")
		}
		account, err := accountFromSteamID(id)
		if err != nil {
			return "", err
		}
		all = append(all, account)
		if fields["mostrecent"] == "1" {
			recent = append(recent, account)
		}
		if fields["autologin"] == "1" {
			auto = append(auto, account)
		}
	}
	for _, candidates := range [][]string{recent, auto, all} {
		if len(candidates) == 1 {
			return candidates[0], nil
		}
		if len(candidates) > 1 {
			return "", fmt.Errorf("Steam 登录账号不唯一，无法确认当前账号；请在 Steam 登录目标账号后重试")
		}
	}
	return "", fmt.Errorf("Steam 登录文件没有可识别的账号；请在 Steam 登录后重试")
}
