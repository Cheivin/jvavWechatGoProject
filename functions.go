package main

import (
	"github.com/eatmoreapple/openwechat"
	"regexp"
	"strings"
)

func searchByName(username string) func(*openwechat.User) bool {
	return func(user *openwechat.User) bool {
		return user.DisplayName == username || user.NickName == username
	}
}

func getUserFromContent[T any](content string, separator string, search func(string) (T, bool)) (searchName string, target T) {
	runeContent := []rune(content)
	if len(runeContent) > 16 { // 微信最大群名片长度16个字符
		content = strings.TrimSpace(string(runeContent[:16]))
	} else {
		content = strings.TrimSpace(content)
	}
	parts := strings.Split(content, separator)
	for i := len(parts); i > 0; i-- {
		searchName = strings.Join(parts[:i], separator)
		if t, ok := search(searchName); ok {
			return searchName, t
		}
	}
	return "", target
}

const (
	quotePrefixZh = "「"
	quoteSuffixZh = "」\n- - - - - - - - - - - - - - -\n"
	quotePrefixEn = "\""
	quoteSuffixEn = "\"\n- - - - - - - - - - - - - - -\n"
)

func getQuote(msgContent string) (string, string, string, bool) {
	if strings.HasPrefix(msgContent, quotePrefixZh) && strings.Contains(msgContent, quoteSuffixZh) {
		// 分离引用内容和正文
		quoteContent := msgContent[len(quotePrefixZh):strings.Index(msgContent, quoteSuffixZh)]
		// 正文部分
		msgContent = strings.TrimPrefix(msgContent, quotePrefixZh+quoteContent+quoteSuffixZh)
		return quoteContent, msgContent, "：", true
	} else if strings.HasPrefix(msgContent, quotePrefixEn) && strings.Contains(msgContent, quoteSuffixEn) {
		// 分离引用内容和正文
		quoteContent := msgContent[len(quotePrefixEn):strings.Index(msgContent, quoteSuffixEn)]
		// 正文部分
		msgContent = strings.TrimPrefix(msgContent, quotePrefixEn+quoteContent+quoteSuffixEn)
		return quoteContent, msgContent, ": ", true
	}
	return "", "", "", false
}

func isRenameGroup(msgContent string) (string, string, bool) {
	if strings.Contains(msgContent, "修改群名为") {
		matches := regexp.MustCompile(`"(.*?)"修改群名为“(.*?)”`).FindStringSubmatch(msgContent)
		if len(matches) > 0 {
			userName := matches[1]
			groupName := matches[2]
			return userName, groupName, true
		}
	} else if strings.Contains(msgContent, "changed the group name to") {
		matches := regexp.MustCompile(`"(.*?)" changed the group name to "(.*?)"`).FindStringSubmatch(msgContent)
		if matches != nil {
			userName := matches[1]
			groupName := matches[2]
			return userName, groupName, true
		}
	}
	return "", "", false
}
