# Thiết kế prompt cache: ba tầng phối hợp litellm / agentcore / ainovel

> Đây là một tài liệu hướng dẫn: giới thiệu cách chúng ta thiết kế prompt cache LLM
> (prompt caching) trên phạm vi đầu-cuối trong ba kho cộng tác, gồm nguyên lý thiết kế,
> các ca truy tìm sự cố thực tế và vị trí mã nguồn đối chiếu được.
>
> - **litellm** — cổng LLM: dịch giao thức và khai báo năng lực
> - **agentcore** — framework Agent: đặt cache và định danh cache
> - **ainovel-cli** — tầng ứng dụng: tích hợp bằng một dòng cấu hình (codebot tương tự)

---

## 1. Vì sao đáng làm: mô hình chi phí và một ca thực tế

Yêu cầu của hệ thống Agent có một đặc điểm cấu trúc: **mỗi lượt yêu cầu đều mang theo toàn bộ lịch sử**. Một vòng lặp tool 30 lượt,
thân yêu cầu của lượt thứ 30 chứa toàn bộ tin nhắn của 29 lượt trước. Khi không có cache, cùng một tiền tố byte bị tính tiền lặp đi lặp lại.

Bảng giá cache của hai hãng lớn (lấy Anthropic làm ví dụ):

| Hạng mục | So với giá đầu vào thường |
|---|---|
| Ghi cache (TTL 5 phút) | 1.25x |
| Ghi cache (TTL 1 giờ) | 2x |
| **Đọc cache** | **0.1x (tiết kiệm 90%)** |

Ca thực tế: một lần sinh tiểu thuyết dài 33 chương tốn $58; phân tích `meta/usage.json` sau đó cho thấy
**tỉ lệ trúng cache toàn bộ chỉ 8.5%** (coordinator chỉ 2.7%, architect là 0). So từng yêu cầu
chuỗi usage (input so với cache_read) để định vị ba nguyên nhân gốc:

1. **Bytes của tools bị rung**: Description/Schema của tool subagent được dựng lại mỗi vòng bằng cách duyệt trực tiếp map của Go,
   thứ tự ngẫu nhiên → thân yêu cầu khác ngay từ byte thứ 0 so với vòng trước → toàn bộ cache tiền tố mất hiệu lực;
2. **Không có ái lực định tuyến**: dòng OpenAI không truyền `prompt_cache_key`, nên các yêu cầu có byte hoàn toàn giống nhau vẫn có thể bị
   cân bằng tải đưa tới một instance không có cache (bằng chứng sắt: trong 33 phiên, chỉ 12 yêu cầu đầu tiên có byte giống nhau trúng cache);
3. **Dòng Claude không có điểm gạch**: Anthropic là cache tường minh, không đánh dấu điểm gạch `cache_control` = hoàn toàn không có cache.

Ba nguyên nhân gốc này tương ứng với ba khối thiết kế phía dưới: **kỷ luật tiền tố ổn định**, **định danh cache**, **điều phối điểm gạch**.

---

## 2. Kiến thức chuẩn bị: mô hình tư duy về hai giao thức cache

### 2.1 OpenAI: cache tiền tố tự động (ẩn)

- Server tự động cache tiền tố **≥1024 token**, không cần client khai báo;
- Lượng trúng tăng dần theo lựa đoạn căn theo 128 token;
- Yêu cầu có thể mang `prompt_cache_key` (trường chính thức) để tạo **ái lực định tuyến** — các yêu cầu cùng key được cố gắng rơi vào
  cùng một mảnh cache;
- Trong usage, `cached_tokens` báo cáo lượng trúng; **ghi cache không bao giờ được báo cáo** (`cache_write` luôn bằng 0
  là hiện tượng bình thường, không phải bug).

### 2.2 Anthropic: điểm gạch tường minh (cache_control)

- Client đánh dấu điểm gạch `cache_control` trên khối nội dung, **điểm gạch phủ toàn bộ những gì trước nó**
  (thứ tự cố định là tools → system → messages);
- Mỗi yêu cầu **tối đa 4 điểm gạch**;
- Giá ghi 1.25x (5m) / 2x (1h), giá đọc 0.1x;
- `cache_control` **không được đặt trên khối thinking** (sẽ bị từ chối với mã 400).

### 2.3 Điều kiện chung

Dù ẩn hay tường minh, cache chỉ công nhận **tiền tố bằng nhau ở mức byte**. Nên nền tảng của mọi thiết kế là cùng một câu:

> **Sắp xếp toàn bộ yêu cầu theo tần suất thay đổi từ thấp đến cao: phần tĩnh đặt trước nhất,
> phần động đặt sau cùng, và lịch sử đã gửi thì không được đổi một byte nào.**

---

## 3. Kiến trúc tổng thể: phân công của ba tầng

```
┌────────────────────────────────────────────────────────┐
│ Tầng ứng dụng (ainovel-cli / codebot)                   │
│   Quyết định "cache identity" lấy giá trị gì:            │
│   một sách một nền, một vai trò một tên                 │
│   Chi phí tích hợp = 2 dòng cấu hình mỗi agent          │
├────────────────────────────────────────────────────────┤
│ agentcore (framework Agent)                             │
│   Quyết định điểm gạch ở đâu, key phái sinh khi nào      │
│   system sàn + đầu mũi lăn; spawn thêm #seq;            │
│   Cổng theo năng lực provider, không hỗ trợ thì bỏ      │
├────────────────────────────────────────────────────────┤
│ litellm (cổng LLM)                                      │
│   Dịch thuần giao thức: cache_control ↔ trường từng hãng │
│   prompt_cache_key truyền thẳng, khai báo Capabilities   │
│   Không quyết định "có cache hay không"                 │
└────────────────────────────────────────────────────────┘
```

Nguyên tắc phân chia: **litellm chỉ trả lời "endpoint này hỗ trợ gì", agentcore chỉ trả lời "đặt điểm cache ở đâu",
tầng ứng dụng chỉ trả lời "định danh là gì"**. Mỗi tầng kiểm thử độc lập được; khi đổi ứng dụng (codebot dùng lại
cùng bộ agentcore/litellm) không phải viết lại logic cache.

---

## 4. Nền tảng: ba kỷ luật về byte của tiền tố

Điều kiện tiên quyết để có lợi ích từ cache là byte của tiền tố ổn định. Mỗi kỷ luật ứng với một tai nạn thực tế.

### Kỷ luật một: tools phải serialize theo byte xác định

Tai nạn: tool `subagent` nhúng danh sách agent đã đăng ký vào Description/Schema của chính nó, mà danh sách đó đến từ
việc duyệt map của Go — thứ tự ngẫu nhiên mỗi lần gọi, byte của tools đổi mỗi vòng, nên tỉ lệ trúng của coordinator chỉ còn 2.7%.
(Đội ngũ Claude Code cũng dính đúng lỗi này: toàn bộ fleet của họ từng phải trả thêm 10.2% chi phí ghi cache.)

Cách sửa (agentcore `subagent/subagent.go`):

```go
// sortedAgentNames returns registered agent names in deterministic order.
// Description and Schema are rebuilt on every LLM call; iterating the map
// directly would shuffle their bytes across requests and defeat provider
// prefix caching (tools serialize into the cached prompt prefix).
func (t *Tool) sortedAgentNames() []string {
	return slices.Sorted(maps.Keys(t.agents))
}
```

> Dạng khái quát của bài học: **mọi tập hợp đi vào thân yêu cầu đều phải được sắp xếp trước khi serialize**. Việc duyệt map
> của Go ngẫu nhiên hoá giấu bug này rất sâu — chức năng hoàn toàn bình thường, chỉ có hoá đơn là bất thường.

### Kỷ luật hai: lịch sử phải append-only (nén thì phải "commit")

Tai nạn: chiến lược nén context của writer là "chiếu" (tạm viết lại lịch sử ở mỗi lượt gọi, nhưng không ghi ngược về
đường cơ sở). Khi vượt ngưỡng, **mỗi vòng lại viết lại toàn bộ tiền tố** → mỗi vòng đều miss.

Cách sửa: commit sau khi chiếu (`CommitOnProject: true`), để việc viết lại chỉ xảy ra một lần, sau đó quay lại
append-only cho tới lần vượt ngưỡng kế tiếp.

> Dạng khái quát: nén context là **một lần đứt có chủ đích** (đặt lại tiền tố, trả giá đầy đủ một lần),
> điều đó không sao; điều không thể chấp nhận là **đứt mỗi vòng**. Nén thì hoặc không làm, hoặc làm xong thì đóng băng.

### Kỷ luật ba: nội dung động phải nằm ở phần đuôi

Mọi thứ đổi theo từng vòng (phong bì trạng thái thế giới, nhắc nhở mỗi vòng, kết quả tool mới nhất) chỉ được **nối thêm ở phần đuôi tin nhắn**,
tuyệt đối không sửa lại phần giữa. Phong bì `novel_context` của ainovel chính là thiết kế nối thêm ở đuôi — nó đổi mỗi chương,
nhưng sự thay đổi đó không ảnh hưởng tới cache của mấy chục nghìn token phía trước.

---

## 5. Định danh cache: một sách một nền, một vai trò một tên, một phiên một key

`prompt_cache_key` của dòng OpenAI giải quyết **vấn đề định tuyến**: yêu cầu có byte giống nhau mà bị cân bằng tải đưa tới
instance khác nhau thì vẫn miss. Mục tiêu thiết kế của key là "mọi yêu cầu cùng một dòng huyết thống cache luôn mang cùng một key".

Ba cấp định danh của chúng ta (ainovel `internal/agents/build.go`):

```go
// promptCacheBase rút ra một hash ngắn ổn định từ thư mục sách, làm tiền tố
// định danh cache của prompt: cùng một cuốn sách dùng chung một bucket định tuyến
// qua các lần khởi động tiến trình, đồng thời không làm lộ đường dẫn cục bộ cho
// provider. Hậu tố vai trò do bên gọi nối thêm, subagent mỗi lần spawn lại
// nối thêm "#seq" (một phiên một key).
func promptCacheBase(bookDir string) string {
	sum := sha256.Sum256([]byte(bookDir))
	return "nvl-" + hex.EncodeToString(sum[:6])
}
```

Tích hợp ở tầng ứng dụng là hai dòng cho mỗi agent:

```go
writer := subagent.Config{
	// ...
	CacheLastMessage: "ephemeral",                // công tắc điểm gạch Claude (xem §6)
	PromptCacheKey:   cacheBase + "-writer",      // định danh định tuyến OpenAI (mức vai trò)
}
// coordinator (Agent cấp cao nhất) tương tự:
agentcore.WithCacheLastMessage("ephemeral"),
agentcore.WithPromptCacheKey(cacheBase+"-coordinator"),
```

Cấp thứ ba (mức phiên) do agentcore tự phái sinh — mỗi lần spawn một phiên mới là một dòng huyết thống cache mới
(agentcore `subagent/subagent.go`):

```go
runSeq := t.runSeq.Add(1)

// One conversation, one cache key: suffix the per-run sequence so each
// spawn forms its own cache lineage instead of piling every run of this
// agent into a single routing bucket.
promptCacheKey := cfg.PromptCacheKey
if promptCacheKey != "" {
	promptCacheKey = fmt.Sprintf("%s#%d", promptCacheKey, runSeq)
}
```

Dạng thành phẩm: `nvl-a1b2c3-writer#17` = cuốn sách này, vai trò writer, phiên lần spawn thứ 17.

> Vì sao không dùng một key toàn cục? Tiền tố của các phiên khác nhau, trộn chung trong một bucket định tuyến sẽ làm loãng tỉ lệ trúng.
> Vì sao không kèm dấu thời gian / số ngẫu nhiên? Key phải **ổn định xuyên các yêu cầu**, mỗi vòng trong phiên đều phải giống nhau.

Thiết kế tương ứng của codebot: ngữ nghĩa key = SessionID (đổi phiên = đổi huyết thống), teammate nối hậu tố tên,
còn khi host dùng lại cùng một instance Agent để đổi phiên thì gọi `Agent.SetPromptCacheKey` để chỉ lại định danh.

---

## 6. Điều phối điểm gạch cho Claude: sàn + đầu mũi lăn

Anthropic không đánh dấu điểm gạch = không có cache. Cách phân bổ ngân sách của chúng ta (tối đa 4 điểm gạch mỗi yêu cầu):

```
[tools][system ←điểm gạch①"sàn"][...lịch sử tin nhắn...][tin nhắn mới nhất ←điểm gạch②"đầu mũi lăn"]
```

### 6.1 Sàn (floor): ghim tiền tố tĩnh

System prompt là khối tĩnh lớn nhất. Cho nó một điểm gạch riêng, bảo đảm **khi phiên mới / cache đuôi bị đuổi,
tiền tố system+tools vẫn còn được đọc từ cache** (agentcore `loop.go`):

```go
} else if agentCtx.SystemPrompt != "" {
	m := SystemMsg(agentCtx.SystemPrompt)
	if config.CacheLastMessage != "" {
		// Cache floor: pin the static system prompt with its own
		// breakpoint so a fresh session — or a turn whose tail entry was
		// evicted — still reads the system+tools prefix from cache.
		m.Metadata = map[string]any{"cache_control": config.CacheLastMessage}
	}
	prefix = append(prefix, m)
}
```

### 6.2 Đầu mũi lăn (rolling tip): mỗi vòng đẩy rộng phần được phủ

Đánh một điểm gạch lên **tin nhắn không phải system cuối cùng**. Trong vòng lặp tool, mỗi lượt gọi LLM đều ghi một
cache phủ tới cặp tool_use+tool_result mới nhất, vòng sau đọc thẳng, không truyền lại:

```go
// markLastMessageForCache returns a copy of messages with cache_control attached
// to the metadata of the last non-system message. System messages are skipped so
// trailing per-turn reminders (which change every turn) don't end up carrying
// the breakpoint.
func markLastMessageForCache(messages []Message, cacheControl string) []Message {
	idx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != RoleSystem {
			idx = i
			break
		}
	}
	// ...
}
```

Lưu ý phải bỏ qua system reminder ở đuôi: nó đổi mỗi vòng, đánh dấu điểm gạch lên nó tức là mỗi vòng ghi một
bản cache không bao giờ được tái sử dụng.

### 6.3 Ngữ nghĩa khối cuối: một tin nhắn chỉ đốt một điểm gạch

Ngữ nghĩa của `cache_control` ở mức tin nhắn là "ghi một điểm gạch sau tin nhắn này". Khi dịch xuống mức khối thì chỉ được
rơi vào **khối cacheable cuối cùng** — đánh dấu mọi khối sẽ đốt hết ngân sách 4 điểm gạch; hơn nữa Anthropic
từ chối khối thinking mang `cache_control`, nên phải quét từ đuôi và bỏ qua reasoning
(agentcore `llm/litellm.go`):

```go
if cache != nil {
	// Anthropic rejects cache_control on thinking blocks — land the
	// breakpoint on the last cacheable block instead.
	for i := len(blocks) - 1; i >= 0; i-- {
		if _, isReasoning := blocks[i].(litellm.ReasoningBlock); isReasoning {
			continue
		}
		blocks[i] = withBlockCache(blocks[i], cache)
		break
	}
}
```

### 6.4 Đường ống TTL

Giá trị cấu hình quy ước là chuỗi `"type[:ttl]"`, ví dụ `"ephemeral"` (mặc định 5m) hoặc `"ephemeral:1h"`:

```go
func cacheControlFromMetadata(metadata map[string]any) *litellm.CacheControl {
	value, _ := metadata["cache_control"].(string)
	if value == "" {
		return nil
	}
	if typ, ttl, ok := strings.Cut(value, ":"); ok {
		return &litellm.CacheControl{Type: typ, TTL: ttl}
	}
	return &litellm.CacheControl{Type: value}
}
```

Có nên nâng lên 1h hay không thì để dữ liệu trả lời: giá ghi nhảy từ 1.25x lên 2x, chỉ đáng nâng khi khoảng cách gọi
đo được thường xuyên vượt 5 phút (chúng ta đo được trung vị của coordinator là 172s, nên không nâng).

---

## 7. Gửi an toàn: cổng năng lực + phán đoán endpoint chính thức

### 7.1 Cổng năng lực: trường không được hỗ trợ thì không ra khỏi cửa

Các provider của litellm **kiểm tra nghiêm ngặt** `ProviderOptions` (khóa lạ báo lỗi thẳng), nên
agentcore cổng theo khai báo năng lực trước khi gửi (agentcore `llm/litellm.go`):

```go
// Prompt-cache routing identity. Capability-gated: litellm providers
// validate provider options strictly, so an unsupported key must be
// dropped here rather than rejected there.
if callCfg.PromptCacheKey != "" && caps.Cache.PromptKey == litellm.SupportYes {
	req.ProviderOptions["prompt_cache_key"] = callCfg.PromptCacheKey
}
```

### 7.2 Phán đoán endpoint chính thức: hệ sinh thái tương thích không có hợp đồng về trường lạ

`prompt_cache_key` là trường chính thức của OpenAI, nhưng hành vi của các endpoint "tương thích OpenAI" không có hợp đồng thống nhất nào.
Khảo sát qua mạng (2026-07):

- **Endpoint nghiêm ngặt từ chối thẳng**: Groq, Cerebras, Volcengine, Fireworks trả về 400/422 cho trường này
  (Zed #36215, OpenClaw #48155 đều vì thế mà đổi sang gửi có điều kiện);
- **Bộ trung gian kiểu dựng lại nhóm (re-compile) âm thầm bỏ**: đường không truyền thẳng của one-api/new-api/sub2api phân tích thân yêu cầu
  vào struct rồi re-marshal, trường lạ biến mất không một tiếng động (gửi đi thì gửi bình thường);
- **Endpoint dễ tính bỏ qua**: Ollama, vLLM bản hiện hành, MiniMax.

Vì vậy khai báo năng lực của provider openai trong litellm được phán đoán **động** theo BaseURL
(litellm `provider/openai/capabilities.go`):

```go
// promptCacheParamsSupport reports whether this endpoint is trusted to accept
// OpenAI's prompt cache params (prompt_cache_key / prompt_cache_retention).
// Only the official endpoint guarantees the field contract.
func (p *Provider) promptCacheParamsSupport() litellm.Support {
	if p.cfg.PromptCacheParams || isOfficialBaseURL(p.cfg.BaseURL) {
		return litellm.SupportYes
	}
	return litellm.SupportUnknown
}

func isOfficialBaseURL(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "api.openai.com")
}
```

`api.openai.com` chính thức → `SupportYes` (có gửi); BaseURL của bên thứ ba → `SupportUnknown`
(cổng ở §7.1 tự động không gửi, **mặc định không bao giờ làm hỏng endpoint nào**); người dùng đã xác nhận
bộ trung gian của họ truyền thẳng nguyên văn thì opt-in tường minh trong cấu hình provider:

```jsonc
"my-relay": {
  "type": "openai",
  "base_url": "https://relay.example.com/v1",
  "extra": { "prompt_cache_params": true }   // tôi xác nhận bộ trung gian này truyền thẳng thân yêu cầu
}
```

> Vì sao công tắc đặt ở tầng năng lực của litellm chứ không phải tầng cấu hình ứng dụng? Vì lúc chạy `/model` đổi provider
> sẽ đổi client, khai báo năng lực đi theo client và tự đổi theo; phán đoán ở thời điểm khởi tạo ứng dụng không bao giờ phủ được
> việc đổi lúc chạy.

---

## 8. Quan sát: phát hiện đứt chuỗi cache

Cache là "tính năng không nhìn thấy" — hỏng thì không báo lỗi, chỉ đắt hơn. Vì vậy cần có quan sát (mượn ý tưởng
promptCacheBreakDetection của Claude Code, làm bản rút gọn).

Cách phán đoán (ainovel `internal/host/usage.go`):

```go
// Trong cùng một phiên (role+task): tiền tố không ngắn lại, mà lượng trúng
// giảm hơn lần trước >5% và mức giảm ≥2000 token
broke := prevPrefix > 0 && prefix >= prevPrefix &&
	float64(u.CacheRead) < float64(prevRead)*cacheBreakKeepRatio &&
	prevRead-u.CacheRead >= cacheBreakMinDropTokens
```

Bốn thiết kế then chốt, mỗi cái ứng với một loại báo động giả:

| Thiết kế | Loại báo động giả mà nó ngăn |
|---|---|
| **Hai ngưỡng** (tương đối 5% và tuyệt đối 2000) | Ngưỡng tương đối đơn lẻ bị nhiễu của tiền tố nhỏ nuốt chửng; ngưỡng tuyệt đối đơn lẻ bỏ sót suy thoái ở tiền tố lớn |
| **Đường cơ sở bám theo phiên (role+task)** | Chiều phát hiện phải khớp với độ mịn phiên của `prompt_cache_key` (`#seq`); so theo role xuyên phiên sẽ báo động giả khi "phiên trước rất ngắn, yêu cầu đầu của phiên mới lại có tiền tố dài hơn" (khoảng trống thật do Codex review phát hiện) |
| **Tiền tố ngắn lại = đặt lại hợp lệ** | Nén context là lần đứt có chủ đích, đặt lại đường cơ sở không cảnh báo |
| **Không phát hiện khi replay** | Replay lịch sử lúc khởi động sẽ quét những lần đứt cũ thành cảnh báo mới |

Khi cảnh báo, phần quy kết nguyên nhân được gợi ý theo khoảng cách thời gian: >1h → nghi do TTL 1h hết hạn; >5m → nghi do TTL 5m hết hạn;
rất ngắn → nghi do server đuổi cache / trôi định tuyến (**bộ trung gian luân phiên tài khoản upstream là nguyên nhân phổ biến nhất**). Bộ đếm được lưu bền vững vào
`usage.json` và hiển thị ở dòng "đứt chuỗi" trong bảng cache của TUI.

---

## 9. Đường đỏ chốt: nguyên tắc đơn điệu của phiên

Một ràng buộc mang tính hiến định cho các tính năng tương lai:

> **Mọi thứ sẽ đi vào tiền tố cache (system prompt, tools, tham số thinking, tham số lấy mẫu),
> sau khi được tính lần đầu trong phiên thì phải đóng băng — cứ để cũ còn hơn làm hỏng cache.**

Ví dụ: tính năng "chỉnh cường độ thinking lúc chạy", nếu để giá trị mới có hiệu lực tức thì với phiên đang diễn ra,
tức là mỗi lần điều chỉnh lại viết lại tiền tố và vô hiệu hoá toàn bộ cache. Cách đúng là giá trị mới chỉ có hiệu lực với
**phiên được spawn mới**. Với mọi yêu cầu kiểu "X chỉnh được lúc chạy", câu hỏi đầu tiên luôn là: X có nằm trong tiền tố cache không?

---

## 10. Những phán đoán sai thường gặp và trần kỹ thuật

1. **`cache_write` của OpenAI luôn bằng 0 là bình thường** — API không báo lượng ghi, đừng đi tìm bug.
2. **Trần của bộ trung gian**: nếu bộ trung gian luân phiên nhiều tài khoản upstream, client có byte ổn định vẫn miss (cache của tài khoản upstream A
   không nhìn thấy được từ tài khoản B). Điều này giải thích bí ẩn "yêu cầu có byte hoàn toàn giống nhau chỉ trúng 12/33".
   **Đây không phải vấn đề client giải được** — dữ liệu của đội ngũ Claude Code cũng cho thấy khoảng chín mươi phần trăm các ca "client không thay đổi
   mà vẫn đứt" là do phía server.
3. **Cách phán đoán**: JSONL của phiên không chứa system prompt và thân yêu cầu đầy đủ, **chuỗi usage từng yêu cầu
   (input so với cache_read) mới là chuẩn vàng để chẩn đoán**. Một dấu vân tay thực dụng: nếu lượng trúng đúng bằng
   "số token của system prompt làm tròn xuống bội số 128", nghĩa là chỉ đoạn system trúng cache, còn đoạn tin nhắn thì miss hết.
4. **Tính lợi ích**: giá đọc 0.1x, giá ghi 1.25x, nghĩa là một bản cache chỉ cần được đọc 1 lần là đã hoàn vốn.
   Trong phiên agent nhiều vòng, điểm gạch hầu như luôn có lợi, nên `CacheLastMessage` không đặt công tắc, mặc định bật.

---

## 11. Hướng dẫn tích hợp — tra nhanh

**ainovel-cli** (đã tích hợp sẵn): mỗi agent cấu hình `CacheLastMessage: "ephemeral"` +
`PromptCacheKey: promptCacheBase(bookDir) + "-<role>"`, phần còn lại tự động.

**codebot** (đã tích hợp sẵn): key = SessionID; khi `Reset`/`SwitchSession` thì
`agent.SetPromptCacheKey(newSessionID)`; teammate dùng `sessionID + "-" + name`.

**Danh sách tối thiểu cho ứng dụng mới nối vào agentcore**:

```go
agentcore.NewAgent(
	agentcore.WithCacheLastMessage("ephemeral"),   // điểm gạch Claude: sàn + đầu mũi lăn
	agentcore.WithPromptCacheKey(stableIdentity),  // định tuyến OpenAI: ổn định, duy nhất theo phiên
	// ...
)
```

Kèm ba câu tự vấn (tương ứng ba kỷ luật):

1. Serialize tools của tôi có xác định ở mức byte không? (mọi tập hợp đã sắp xếp chưa)
2. Lịch sử của tôi có append-only không? (nén có commit không)
3. Nội dung đổi mỗi vòng của tôi đã nằm ở phần đuôi chưa?

---

## 12. Danh sách kinh nghiệm dành cho người học

- Bản chất của việc tối ưu cache là **kỷ luật byte**, không phải chỉnh tham số: bảo đảm tiền tố ổn định trước, rồi mới bàn tới key và điểm gạch.
- Chẩn đoán luôn bắt đầu từ **chuỗi usage từng yêu cầu**, đừng đoán từ mã nguồn.
- Việc duyệt map của Go ngẫu nhiên hoá + serialize thân yêu cầu = kẻ giết cache tinh vi nhất, kiểm thử chức năng không bao giờ phát hiện ra.
- "Tương thích OpenAI" là từ tiếp thị, không phải hợp đồng: trước khi gửi trường chính thức tới endpoint bên thứ ba, hãy tìm bằng chứng nguồn gốc
  (mã nguồn / issue / cách sửa đã triển khai ở client tương tự), "thường sẽ bị bỏ qua" là suy luận nguy hiểm.
- Quan sát phải ưu tiên chống báo động giả: chiều phát hiện phải khớp với độ mịn của dòng huyết thống cache; thà bỏ sót còn hơn báo động giả,
  nếu không cảnh báo sẽ nhanh chóng bị phớt lờ.
- Tiêu chí kiểm tra của tính phân tầng: khi một ứng dụng khác (codebot) tích hợp, không phải viết lại dòng logic cache nào.

---

### Phụ lục: chỉ mục mã nguồn

| Chủ đề | Vị trí |
|---|---|
| Sắp xếp xác định cho tools | agentcore `subagent/subagent.go` `sortedAgentNames` |
| Phái sinh key mức phiên (#seq) | agentcore `subagent/subagent.go` `runAgent` |
| Sàn system + đầu mũi lăn | agentcore `loop.go` `callLLM` / `markLastMessageForCache` |
| Điểm gạch khối cuối + bỏ qua thinking | agentcore `llm/litellm.go` `convertAgentBlocks` |
| Phân tích TTL ("ephemeral:1h") | agentcore `llm/litellm.go` `cacheControlFromMetadata` |
| Cổng năng lực | agentcore `llm/litellm.go` `applyCallConfig` |
| Phán đoán endpoint chính thức + opt-in | litellm `provider/openai/capabilities.go` / `provider.go Config` |
| Định danh cache (một sách một nền) | ainovel `internal/agents/build.go` `promptCacheBase` |
| Phát hiện đứt chuỗi | ainovel `internal/host/usage.go` `noteCacheBreak` |
| Định vị kiến trúc | ainovel `docs/architecture.md` §6.6 |
