package chprov

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"time"
)

// 本文件是两家**要求请求签名**的渠道的鉴权实现：
//
//	codearts  华为云 SDK-HMAC-SHA256（AK/SK + securityToken）
//	loomy     讯飞 CAccount 的 account {ak}:{sig}（HMAC-SHA1）
//
// # 为什么这两种签名必须逐字节复刻
//
// 签名是**字节级契约**：少一个换行、多一次 URL 编码、`+` 被当空格，服务端算出的
// canonical 就与我们的分叉，返回 401 verify ak sk signature fail。而那类失败与
// 「凭据真失效」逐字同形 —— 只看错误码会把人引到「重新登录」的错误方向。
// 参考实现为此写死了逐字节的拼串格式并用单测锁定；这里照同样的做法。

// --- 华为云 SDK-HMAC-SHA256 ---------------------------------------------------

// signHuawei 就地写入华为云签名头。
//
// 算法（与官方文档同形，逐字节复刻参考实现）：
//
//	CanonicalRequest = METHOD \n URI \n CanonicalQuery \n HeaderLines \n
//	                   "" \n SignedHeaders \n hex(SHA256(body))
//	StringToSign     = "SDK-HMAC-SHA256" \n X-Sdk-Date \n hex(SHA256(CanonicalRequest))
//	Signature        = hex(HMAC-SHA256(SK, StringToSign))
//	Authorization    = SDK-HMAC-SHA256 Access=AK,SignedHeaders=…,Signature=…
//
// ⚠️ CanonicalQuery 必须**按 key 字典序**（实测：wire 顺序与签名顺序一致时反而
// 401，改成签名内排序后 200，四轮 4/4 确定性）。
// ⚠️ URI 必须**带尾斜杠**。
// ⚠️ 参与签名的头不含调用方的 X-Snap-* 附加头：它们签名后追加，带上即验签失败。
func signHuawei(dst map[string]string, method, rawURL string, s QuotaSpec, a Account) error {
	ak := a.Field("access_key_id")
	sk := a.Field("secret_access_key")
	st := a.Field("security_token")
	if ak == "" || sk == "" {
		return errText("凭据缺少 AK/SK")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	uri := u.Path
	if uri == "" {
		uri = "/"
	}
	if !strings.HasSuffix(uri, "/") {
		uri += "/"
	}
	dateStamp := time.Now().UTC().Format("20060102T150405Z")
	payload := s.Body
	sum := sha256.Sum256([]byte(payload))
	payloadHash := hex.EncodeToString(sum[:])

	headerSet := map[string]string{
		"host":                 u.Host,
		"x-sdk-date":           dateStamp,
		"x-sdk-content-sha256": payloadHash,
	}
	if st != "" {
		headerSet["x-security-token"] = st
	}
	if !strings.EqualFold(method, "GET") {
		headerSet["content-type"] = "application/json"
	}
	keys := make([]string, 0, len(headerSet))
	for k := range headerSet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	signedHeaders := strings.Join(keys, ";")
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+":"+headerSet[k])
	}
	canonical := strings.Join([]string{
		strings.ToUpper(method), uri, canonicalQuery(u.RawQuery), strings.Join(lines, "\n"), "",
		signedHeaders, payloadHash,
	}, "\n")
	ch := sha256.Sum256([]byte(canonical))
	stringToSign := "SDK-HMAC-SHA256\n" + dateStamp + "\n" + hex.EncodeToString(ch[:])
	mac := hmac.New(sha256.New, []byte(sk))
	mac.Write([]byte(stringToSign))
	auth := "SDK-HMAC-SHA256 Access=" + ak + ",SignedHeaders=" + signedHeaders +
		",Signature=" + hex.EncodeToString(mac.Sum(nil))

	// 请求头：签名里的那几个 + Authorization。`host` 由运行时按连接目标生成，
	// 手工设置会被忽略/拒绝，故不回填。
	dst["x-sdk-date"] = dateStamp
	dst["x-sdk-content-sha256"] = payloadHash
	if st != "" {
		dst["x-security-token"] = st
	}
	dst["authorization"] = auth
	if !strings.EqualFold(method, "GET") {
		dst["content-type"] = "application/json"
	}
	return nil
}

// canonicalQuery 把 query 规范成 **key 排序** 且逐段百分号编码的形态。
//
// ⚠️ 只解 %XX、绝不解 `+`：解 `+` 会让它与服务端的口径分叉（表单语义下 `+` 是
// 空格），而 `PathUnescape` 不做那件事。故这里只按 %XX 往返。
func canonicalQuery(search string) string {
	search = strings.TrimPrefix(search, "?")
	if search == "" {
		return ""
	}
	type kv struct{ k, v string }
	pairs := []kv{}
	for _, part := range strings.Split(search, "&") {
		if part == "" {
			continue
		}
		k, v := part, ""
		if i := strings.Index(part, "="); i >= 0 {
			k, v = part[:i], part[i+1:]
		}
		dk, _ := url.PathUnescape(k)
		dv, _ := url.PathUnescape(v)
		pairs = append(pairs, kv{dk, dv})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, escapeStrict(p.k)+"="+escapeStrict(p.v))
	}
	return strings.Join(out, "&")
}

// escapeStrict 是 RFC3986 转义（把 QueryEscape 的 + 换回 %20）。
func escapeStrict(v string) string {
	e := url.QueryEscape(v)
	return strings.ReplaceAll(e, "+", "%20")
}

// --- 讯飞 CAccount（loomy）----------------------------------------------------

// signLoomy 就地写入讯飞账号签名头。
//
// 字串**9 段**，\n 连接，**后两段恒为空**（本项目不发任何 x-* 头）：
//
//	METHOD \n ESCAPED_PATH \n ESCAPED_QUERY \n Content-MD5 \n
//	Content-Type \n Date \n Nonce \n "" \n ""
//
// ⚠️ 以两个换行结尾，别「顺手」去掉 —— 去掉即签名不匹配。
// ⚠️ 认证头前缀是 account（account {ak}:{sig}），**不是** Bearer。
// ⚠️ query **不排序**（保持传入顺序），与华为云那条相反。
func signLoomy(dst map[string]string, method, path string, s QuotaSpec, a Account) error {
	ak := a.Field("access_key")
	sk := a.Field("secret_key")
	if ak == "" || sk == "" {
		return errText("凭据缺少讯飞 access_key / secret_key")
	}
	rawPath, rawQuery := path, ""
	if i := strings.Index(path, "?"); i >= 0 {
		rawPath, rawQuery = path[:i], path[i+1:]
	}
	date := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	nonce := uuidV4()
	contentType := "application/json"
	body := s.Body
	md5sum := ""
	if body != "" {
		h := md5.Sum([]byte(body))
		md5sum = base64.StdEncoding.EncodeToString(h[:])
	}
	stringToSign := strings.Join([]string{
		strings.ToUpper(method),
		buildEscapedPath(rawPath),
		buildEscapedQuery(rawQuery),
		md5sum,
		contentType,
		date,
		nonce,
		"",
		"",
	}, "\n")
	mac := hmac.New(sha1.New, []byte(sk))
	mac.Write([]byte(stringToSign))
	dst["authorization"] = "account " + ak + ":" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	dst["date"] = date
	dst["nonce"] = nonce
	dst["content-type"] = contentType
	if md5sum != "" {
		dst["content-md5"] = md5sum
	}
	return nil
}

// buildEscapedPath 补前导斜杠、剥末尾斜杠（长度 > 1 时）、逐段严格转义。
func buildEscapedPath(raw string) string {
	clean := raw
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	if len(clean) > 1 && strings.HasSuffix(clean, "/") {
		clean = clean[:len(clean)-1]
	}
	segs := strings.Split(clean, "/")
	for i, seg := range segs {
		if seg != "" {
			segs[i] = escapeRFC3986(seg)
		}
	}
	return strings.Join(segs, "/")
}

// buildEscapedQuery 逐对转义、**保持传入顺序**（与华为云那条相反，别照抄）。
func buildEscapedQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := []string{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		k, v := pair, ""
		if i := strings.Index(pair, "="); i >= 0 {
			k, v = pair[:i], pair[i+1:]
		}
		parts = append(parts, escapeRFC3986(k)+"="+escapeRFC3986(v))
	}
	return strings.Join(parts, "&")
}

// escapeRFC3986 是 encodeURIComponent 的等价形态（QueryEscape 把 + 换回 %20，
// 再补转 ! ' ( ) * 这五个 encodeURIComponent 不编码的字符）。
func escapeRFC3986(v string) string {
	e := url.QueryEscape(v)
	e = strings.ReplaceAll(e, "+", "%20")
	for _, pair := range [][2]string{
		{"!", "%21"}, {"'", "%27"}, {"(", "%28"}, {")", "%29"}, {"*", "%2A"},
	} {
		e = strings.ReplaceAll(e, pair[0], pair[1])
	}
	return e
}

// errText 是 errors.New 的短别名，只为让两处签名入口读起来一致。
func errText(s string) error { return simpleErr(s) }

// simpleErr 是零分配的错误实现（不引 errors 包，本文件因此少一个 import）。
type simpleErr string

func (e simpleErr) Error() string { return string(e) }

// uuidV4 生成一个随机 UUID v4。
func uuidV4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0F) | 0x40
	b[8] = (b[8] & 0x3F) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
