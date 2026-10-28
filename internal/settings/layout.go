package settings

import "strings"

// 管理后台中设置项的排列：分类 → 小节 → 设置项。每个小节先列常用的，以「~」开头的是高级设置（默认折叠）。
// 没有列出的设置项放在「其他」里（防止新增的配置在后台找不到），hidden 中的不显示。

// Group 是设置的一个分类（管理后台左侧目录中的一项）。
type Group struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Help 是这个分类的说明。
	Help string `json:"help,omitempty"`
}

type section struct {
	group, label string
	keys         []string
}

// Groups 是设置分类，按展示顺序排列。
var Groups = []Group{
	{"models", "模型", "这里是配置文件中的模型；在网页「设置 → 模型」里添加的模型不在这里管理"},
	{"chat", "聊天与上下文", "长对话怎么压缩、每天能聊多少、用户能选哪些性格"},
	{"memory", "记忆与情绪", ""},
	{"tools", "工具", "联网搜索与运行代码"},
	{"proactive", "提醒与主动消息", ""},
	{"channels", "聊天软件", "微信、QQ、Telegram 的接入开关；连接情况在左侧「聊天软件连接」里看"},
	{"files", "文件", ""},
	{"account", "账号与安全", ""},
	{"server", "服务器", "网络、数据库、更新与日志"},
	{"other", "其他", ""},
}

var layout = []section{
	{"models", "聊天模型", []string{"llm.chat.base_url", "llm.chat.api_key", "llm.chat.model", "llm.chat.context_tokens", "llm.chat.vision", "llm.chat.tools",
		"llm.chat.thinking_style", "~llm.chat.thinking_model", "~llm.chat.provider", "~llm.chat.temperature", "~llm.chat.max_tokens", "~llm.chat.timeout"}},
	{"models", "辅助模型（整理记忆、摘要等后台任务）", []string{"llm.utility.model", "llm.utility.base_url", "llm.utility.api_key", "~llm.utility.context_tokens",
		"~llm.utility.provider", "~llm.utility.thinking_style", "~llm.utility.thinking_model", "~llm.utility.vision", "~llm.utility.tools",
		"~llm.utility.temperature", "~llm.utility.max_tokens", "~llm.utility.timeout"}},
	{"models", "向量模型", []string{"llm.embedding.base_url", "llm.embedding.api_key", "llm.embedding.model",
		"~llm.embedding.provider", "~llm.embedding.dim", "~llm.embedding.batch_size", "~llm.embedding.timeout"}},

	{"chat", "上下文", []string{"chat.summarize", "chat.compact_at", "chat.keep_recent"}},
	{"chat", "每日限额", []string{"quota.daily_messages", "quota.daily_tokens", "quota.exempt_admins"}},
	{"chat", "性格", []string{"persona.disabled_presets", "~persona.file"}},

	{"memory", "记忆", []string{"memory.write.enabled", "memory.write.importance_threshold", "memory.retrieval.top_k", "memory.compact.max_memories", "~memory.compact.similarity",
		"~memory.retrieval.min_relevance", "~memory.retrieval.candidate_k", "~memory.retrieval.alpha", "~memory.retrieval.beta", "~memory.retrieval.gamma",
		"~memory.retrieval.decay", "~memory.write.dedup_similarity", "~memory.write.access_boost", "~memory.write.workers", "~memory.write.queue_size",
		"~memory.cache.max_users", "~memory.cache.max_bytes", "~memory.cache.ttl", "~scheduler.enabled", "~scheduler.consolidation_spec"}},
	{"memory", "情绪", []string{"emotion.enabled", "emotion.expressiveness", "emotion.temper", "emotion.patience", "emotion.forgiveness", "emotion.empathy"}},

	{"tools", "联网搜索", []string{"tools.search.provider", "tools.search.deepseek_api_key", "tools.search.bocha_api_key", "tools.search.tavily_api_key", "~tools.search.direct_fetch"}},
	{"tools", "运行代码", []string{"tools.python.runner", "tools.python.packages", "tools.python.allow_install", "tools.python.index_url", "tools.python.workspace_mb",
		"~tools.python.timeout", "~tools.python.memory_mb", "~tools.python.image", "~tools.python.docker", "~tools.python.python", "~tools.python.lib_dir",
		"~tools.python.lib_max_mb", "~tools.python.workspace_dir", "~tools.python.install_timeout"}},

	{"proactive", "提醒与主动关心", []string{"proactive.reminders", "proactive.care", "proactive.quiet_start", "proactive.quiet_end",
		"~proactive.idle_hours", "~proactive.min_gap_hours", "~proactive.check_interval"}},

	{"channels", "个人微信", []string{"channels.weixin.enabled"}},
	{"channels", "QQ", []string{"channels.qq.enabled", "channels.qq.access_token", "~channels.qq.auto_accept_friend"}},
	{"channels", "Telegram", []string{"channels.telegram.enabled", "channels.telegram.bot_token", "channels.telegram.proxy", "~channels.telegram.api_base"}},
	{"channels", "存储", []string{"~channels.dir"}},

	{"files", "上传", []string{"uploads.max_file_mb", "uploads.user_quota_mb", "uploads.max_files", "~uploads.doc_chars", "~uploads.dir"}},

	{"account", "登录与注册", []string{"mode", "auth.access_password", "auth.registration", "auth.jwt_secret", "~auth.token_ttl"}},
	{"account", "介绍主页（多用户版未登录时显示）", []string{"home.title", "home.tagline"}},
	{"account", "安全", []string{"security.allow_private_model_urls", "~admin.password", "~security.encryption_key"}},

	{"server", "网络", []string{"timezone", "server.listen", "server.base_url", "~server.cors_origins", "~server.read_timeout", "~server.write_timeout"}},
	{"server", "数据库", []string{"database.driver", "database.dsn", "~database.max_open_conns", "~database.max_idle_conns", "~database.log_sql"}},
	{"server", "更新", []string{"update.check_on_start", "update.mirror", "~update.repo"}},
	{"server", "日志与调试", []string{"log.level", "~log.format", "~debug.prompt_endpoint", "~data_dir"}},
}

// hidden 是已经不再使用、只为兼容旧配置文件保留的设置。
var hidden = map[string]bool{"chat.history_messages": true, "chat.max_context_tokens": true}

// units 是数字设置的单位（显示在输入框后面）。
var units = map[string]string{
	"chat.compact_at": "%", "chat.keep_recent": "%",
	"quota.daily_messages": "条", "quota.daily_tokens": "百万 token",
	"llm.chat.max_tokens": "token", "llm.utility.max_tokens": "token",
	"memory.retrieval.top_k": "条", "memory.compact.max_memories": "条", "memory.retrieval.candidate_k": "条", "memory.cache.max_bytes": "字节",
	"tools.python.memory_mb": "MB", "tools.python.lib_max_mb": "MB", "tools.python.workspace_mb": "MB",
	"uploads.max_file_mb": "MB", "uploads.user_quota_mb": "MB", "uploads.max_files": "个", "uploads.doc_chars": "字",
	"proactive.idle_hours": "小时", "proactive.min_gap_hours": "小时",
}

// scales 是网页上按大单位填写的数字设置（见 Field.Scale）。
var scales = map[string]int64{"quota.daily_tokens": 1_000_000}

type place struct {
	group, section string
	advanced       bool
	order          int
}

var places = func() map[string]place {
	out := map[string]place{}
	n := 0
	for _, s := range layout {
		for _, k := range s.keys {
			adv := strings.HasPrefix(k, "~")
			out[strings.TrimPrefix(k, "~")] = place{group: s.group, section: s.label, advanced: adv, order: n}
			n++
		}
	}
	return out
}()

// placeOf 返回设置项在后台中的位置；没有列出的放在「其他」，算作高级设置。
func placeOf(key string) place {
	if p, ok := places[key]; ok {
		return p
	}
	return place{group: "other", section: "其他", advanced: true, order: len(places)}
}
