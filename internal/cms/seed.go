package cms

import (
	"context"
	"time"
)

// SeedDemo 载入内置的布谷中文示例文章。按 slug 判重，已存在的文章会被跳过且不会被覆盖。
func (s *Service) SeedDemo(ctx context.Context) (int, error) {
	now := s.now().UTC()
	samples := demoPosts(now)
	created := 0

	for _, in := range samples {
		var count int64
		if err := s.db.WithContext(ctx).Model(&postRow{}).Where("slug = ?", in.Slug).Count(&count).Error; err != nil {
			return created, err
		}
		if count > 0 {
			continue
		}
		if _, err := s.Create(ctx, in); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}

func demoPosts(base time.Time) []PostInput {
	t1 := base.Add(-4 * time.Hour)
	t2 := base.Add(-28 * time.Hour)
	t3 := base.Add(-72 * time.Hour)
	t4 := base.Add(-120 * time.Hour)
	t5 := base.Add(-168 * time.Hour)

	return []PostInput{
		{
			Slug:        "buugoo-memory-architecture",
			Category:    "技术",
			Tags:        []string{"长期记忆", "向量检索", "架构设计"},
			Title:       "布谷如何记住与你的每一次对话：语义检索与时间衰减机制",
			Summary:     "拆解布谷背后的长期记忆读写流程，说明向量相似度、记忆强度与定期整理如何共同维持自然、克制的回忆体验。",
			Author:      "布谷工程团队",
			CoverTheme:  ThemeObsidian,
			Status:      StatusPublished,
			Featured:    true,
			Pinned:      true,
			SortOrder:   10,
			PublishedAt: &t1,
			Content: `## 为什么对话式 AI 需要真正的长期记忆

普通聊天窗口在刷新或新建会话后便会忘记上下文，用户不得不一次次重复自己的工作习惯、生活节奏与偏好。布谷的设计目标不是做一次性的问答工具，而是成为能长期相伴的 AI 朋友。

要让回忆显得自然而不是生硬地背诵档案，记忆系统必须同时解决三个问题：

- **写入克制**：只提炼值得记住的长期信息，过滤一次性寒暄。
- **检索精准**：在当前语境下找到最相关的几条片段，而不是把所有历史塞进提示词。
- **持续整理**：定期合并重复事实，降低过时细节的权重。

## 混合相关性与时间衰减模型

在检索阶段，布谷会综合候选记忆 $m$ 与当前查询 $q$ 的语义相似度、记忆强度以及距今的时间间隔 $\Delta t$ 计算综合得分：

$$
s(m, q) = \alpha \cdot \frac{\mathbf{v}_m \cdot \mathbf{v}_q}{\|\mathbf{v}_m\| \|\mathbf{v}_q\|} + (1 - \alpha) \cdot w_m \cdot e^{-\lambda \Delta t}
$$

其中 $\alpha \in [0, 1]$ 控制语义相关与时效记忆的比重，$w_m$ 表示单条记忆的重要性与被访问频次加权，$\lambda > 0$ 为衰减系数。经常被提及的偏好会保持较高的 $w_m$，而偶发的一次性事件会随时间自然淡出。

## 工程实现上的取舍

为了让个人服务器也能轻快运行，布谷采用单二进制架构并内置内存向量缓存，所有记忆均落盘保存在自己的 SQLite 或 MySQL 数据库中：

` + "```yaml" + `
memory:
  retrieval:
    top_k: 8
    min_score: 0.35
  compact:
    enabled: true
` + "```" + `

> 记忆数据完全归用户自己所有，你可以随时在网页端的「记忆」页面浏览、检索或删除任何一条记录。`,
		},
		{
			Slug:        "buugoo-emotion-and-persona",
			Category:    "产品",
			Tags:        []string{"情绪引擎", "性格预设", "陪伴体验"},
			Title:       "有脾气也有温度：布谷情绪引擎与性格预设的设计思路",
			Summary:     "介绍布谷如何在连续对话中保持情绪起伏与角色一致性，让交流不再停留在机械礼貌的模板回复。",
			Author:      "布谷产品团队",
			CoverTheme:  ThemeAurora,
			Status:      StatusPublished,
			Featured:    true,
			Pinned:      false,
			SortOrder:   20,
			PublishedAt: &t2,
			Content: `## 从「工具腔」走向自然交流

多数对话模型默认采用高度客套、四平八稳的客服语气。虽然礼貌，但在日常陪伴场景里往往显得疏离。布谷内置了独立的情绪引擎与性格预设体系，希望让每一次回应都带着连贯的状态。

## 情绪维度与动态衰减

布谷在每次交互后都会对对话氛围进行轻量评估，并按不同性格预设的灵敏度更新内部情绪状态：

| 性格维度 | 影响方向 | 典型表现 |
| --- | --- | --- |
| 表达度 | 情绪外露程度 | 回复中语气与情感色彩的浓淡 |
| 耐心值 | 负面情绪阈值 | 面对反复打断或冷淡回应时的容忍范围 |
| 宽容度 | 情绪恢复速度 | 争执后重新缓和关系的快慢 |
| 共情力 | 对方情绪响应 | 察觉疲惫或焦虑时主动关心的倾向 |

随着时间推移，波动的情绪会逐步回归平稳基线，不会因为一次偶发误解永远停留在低谷。

## 自由定制人格档案

除了开箱即用的内置性格预设，管理员和个人用户还可以在管理后台或设置页面调整人格档案，甚至按需启用 Cosplay 角色卡，在独立会话里体验截然不同的相处方式。`,
		},
		{
			Slug:        "buugoo-arena-benchmark-notes",
			Category:    "动态",
			Tags:        []string{"BuugooArena", "中文评测", "盲测"},
			Title:       "BuugooArena 评测手记：为什么我们关注「网感」与「说人话」",
			Summary:     "分享旗下公开评测平台 BuugooArena 的设计初衷，用双盲投票与统计排名观察主流模型在真实中文语境下的表现。",
			Author:      "布谷评测组",
			CoverTheme:  ThemeCobalt,
			Status:      StatusPublished,
			Featured:    true,
			Pinned:      false,
			SortOrder:   30,
			PublishedAt: &t3,
			Content: `## 跑分之外的真实体感

许多公开基准擅长考察代码补全和竞赛数学，但用户在日常聊天里最在意的往往是另外两件事：能不能听懂当下的中文梗，以及能不能把复杂问题讲成通俗易懂的人话。

为此，我们推出了公开盲测平台 **BuugooArena**，聚焦两个贴近真实交流的视角：

1. **网感与接梗**：考察模型对中文互联网语境、隐喻与轻松调侃的理解力。
2. **说人话**：考察模型能否剥离套话与翻译腔，用清晰直白的中文把事情讲清楚。

## 盲测评分与胜率估计

在双盲对比中，参与者看不到模型名称，只根据回答质量投票。对于评分为 $R_A$ 与 $R_B$ 的两个模型，模型 $A$ 的预期胜率满足：

$$
E_A = \frac{1}{1 + 10^{(R_B - R_A)/400}}
$$

随着盲测样本持续累积，榜单能够更稳健地反映不同模型在真实中文对话中的相对优势，而不是单一提示词下的偶然发挥。

你可以随时前往 [BuugooArena](https://arena.buugoo.com) 查看最新结果或亲自参与盲测投票。`,
		},
		{
			Slug:        "getting-started-with-buugoo",
			Category:    "指南",
			Tags:        []string{"快速上手", "多端接入", "定时提醒"},
			Title:       "十分钟上手布谷：模型配置、备忘录与多端随处可聊",
			Summary:     "面向新用户的实用指南，覆盖添加聊天模型、管理提醒与待办，以及绑定微信、QQ 或 Telegram 的完整步骤。",
			Author:      "布谷文档组",
			CoverTheme:  ThemeSage,
			Status:      StatusPublished,
			Featured:    false,
			Pinned:      false,
			SortOrder:   40,
			PublishedAt: &t4,
			Content: `## 第一步：配置聊天与向量模型

登录布谷后，前往「设置 → 模型」添加你的第一个模型供应商。填入接口地址与 API Key 后即可拉取远程模型列表并设为默认聊天模型。

建议同时配置一个轻量的向量模型（Embedding），这样布谷的长期记忆检索能力就会自动开启。

## 第二步：让布谷接管提醒、待办与备忘录

在对话中直接用自然语言交代事务即可，例如：

- 「明早九点提醒我提交周报」
- 「记一条备忘录：下周三前核对服务器续费时间」
- 「帮我列一份周末出行的待办清单」

布谷会自动调用内置工具记录并到点提醒，你也可以在网页侧栏随时查看、编辑或勾选完成。

## 第三步：接入常用聊天软件

如果希望在手机上随时联系布谷，可以在管理后台开启对应通道，随后在个人设置中生成一次性绑定码，即可将同一份记忆与性格延续到微信、QQ 或 Telegram 中。`,
		},
		{
			Slug:        "buugoo-self-hosting-backup-guide",
			Category:    "指南",
			Tags:        []string{"自托管", "数据库迁移", "运维备份"},
			Title:       "布谷自托管与数据迁移指南：从 SQLite 平滑升级到 MySQL",
			Summary:     "整理布谷单二进制部署、一键升级与数据库备份的关键注意事项，帮助站长稳妥维护单用户与多用户实例。",
			Author:      "布谷工程团队",
			CoverTheme:  ThemeSand,
			Status:      StatusPublished,
			Featured:    false,
			Pinned:      false,
			SortOrder:   50,
			PublishedAt: &t5,
			Content: `## 轻装起步，按需扩展

布谷默认使用内置 SQLite 数据库，单文件即可启动，几乎零运维负担，非常适合个人自用或小规模试用。当站点的注册用户和对话记录逐渐增长时，可以随时无缝迁移到 MySQL 8.4。

## 备份清单

在执行版本升级或迁移之前，建议完整备份以下三类核心数据：

1. **数据库文件或 MySQL 导出快照**：保存用户、会话、记忆与文章内容。
2. **本地加密密钥文件**：位于数据目录内，用于解密网页中添加的模型 API Key，必须与数据库一并妥善保管。
3. **配置文件与上传目录**：包含站点参数、自定义人格档案与用户上传附件。

## 一键在线迁移到 MySQL

在管理后台的「数据库」页面中，填入目标 MySQL 连接信息并点击测试连接，确认通过后即可触发在线迁移。迁移过程中站点会自动进入只读维护状态，确保数据完整一致，完成后重启服务即可切换至新数据库运行。`,
		},
	}
}
