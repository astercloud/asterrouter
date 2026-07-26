package controlplane

import "strings"

// 邮箱别名归一化：用于注册去重。
//
// 问题：注册查重按邮箱原值比较（仅小写+去空白），单个真实收件箱可以借服务商的
// 别名特性无限派生账号：
//   - 加号别名：user+tag@gmail.com 投递到 user@gmail.com（Gmail、Outlook、
//     Yahoo、iCloud、Fastmail 等均支持）。
//   - Gmail 点号：u.s.e.r@gmail.com 投递到 user@gmail.com。
//   - FQDN 根点：user@gmail.com. 是 user@gmail.com 的绝对形式，能通过校验且
//     到达同一邮箱。
//
// 这让攻击者可以批量注册来刷新用户默认余额，而域名白名单与邮件验证会把每个
// 变体都视为独立且可投递的地址。NormalizeEmailForAliasDedup 把这些变体折叠成
// 同一个“收件箱标识”，供注册路径拒绝重复。
//
// 归一化规则：
//   - 所有域名：小写、去空白、去掉 FQDN 根点、剥离 local 部分的 "+后缀"。
//     对不支持加号别名的域名剥离后缀是无害的——那些精确地址几乎不会被注册。
//   - Gmail 系（gmail.com / googlemail.com）：额外去掉 local 部分的点，域名
//     折叠为 gmail.com。
//
// 该归一化只作用于注册查重，不改变邮箱的存储、展示以及登录/投递用值。

var gmailFamilyDomains = map[string]struct{}{
	"gmail.com":      {},
	"googlemail.com": {},
}

// NormalizeEmailForAliasDedup 返回邮箱的规范“收件箱标识”。
// 非法输入原样小写去空白返回，格式校验由调用方负责。
func NormalizeEmailForAliasDedup(email string) string {
	local, domain, ok := splitEmailForAliasDedup(email)
	if !ok {
		return strings.ToLower(strings.TrimSpace(email))
	}
	local = stripEmailPlusSuffix(local)
	if _, isGmail := gmailFamilyDomains[domain]; isGmail {
		local = stripEmailLocalDots(local)
		domain = "gmail.com"
	}
	return local + "@" + domain
}

func splitEmailForAliasDedup(email string) (local string, domain string, ok bool) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(normalized, "@")
	if at < 0 {
		return "", "", false
	}
	local, domain = normalized[:at], strings.TrimRight(normalized[at+1:], ".")
	if local == "" || domain == "" {
		return "", "", false
	}
	return local, domain, true
}

// stripEmailPlusSuffix 剥离 local 部分的 "+后缀"。
// 只处理 idx > 0：local 形如 "+tag" 剥离后为空，把所有 "+x@host" 折叠成
// "@host" 会让该域名下无关的发件人互相冲突。
func stripEmailPlusSuffix(local string) string {
	if idx := strings.IndexByte(local, '+'); idx > 0 {
		return local[:idx]
	}
	return local
}

// stripEmailLocalDots 去掉 local 部分的点；全是点时保留原值（无服务商会投递）。
func stripEmailLocalDots(local string) string {
	if stripped := strings.ReplaceAll(local, ".", ""); stripped != "" {
		return stripped
	}
	return local
}
