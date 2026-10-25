# 视觉设计

布谷的 Web 界面与终端界面统一使用设计系统 **布谷 BUUGOO**：
https://claude.ai/artifact/Fn5TtFo5aLDYVpM9ZWujUp

设计系统是唯一的来源，本仓库只消费它，不另起一套。

## 对应关系

| 设计系统 | 本仓库 |
| --- | --- |
| `tokens.json`（颜色、字体、间距、圆角、阴影） | `web/src/tokens.css`，由 `scripts/gen-tokens.py` 生成，不要手改 |
| `components/bundle.css` 的组件类名 | `web/src/style.css`：`.btn` / `.btn-primary` / `.field` / `.text-input` / `.type-switch` / `.chip-note` / `.badge-pill` / `.panel` / `.toast` / `.app-rail` / `.workspace-progress` 等，类名与设计系统一致 |
| Logos 组 `logobg.png`、`icon-512.png` | `web/public/logo.png`（128px 缩小版）、`web/public/favicon.png`、`web/public/icon-512.png` |
| Icons 组（24px 网格、1.8 描边） | `web/src/Icon.vue`，内联 SVG，颜色继承 `currentColor` |
| 颜色 | `internal/tui/tui.go` 终端配色、`internal/httpapi/static.go` 未内嵌前端时的提示页 |

## 落地时遵守的规则（摘自设计系统 README）

- 页面底色 `color-canvas`，功能面板白色 `color-surface`，导航栏与切换轨道 `color-canvas-soft`。
- 主按钮是墨色（`color-ink`），**不是绿色**；橄榄绿 `color-accent` 只作为信号：输入框聚焦、进度条、手机底栏的当前项、「想起了 N 件事」的 `chip-note`。
- 标题用衬线 display 字体、字重 400，不加粗；正文用系统无衬线字体；不加载网络字体。
- 圆角：按钮与输入框 8px，导航项 10px，面板与提示 12px；胶囊形只用于标签、切换与进度条。
- 每个面只用一种边界（底色对比、一条细线或柔和阴影三选一）；不在白色卡片里再套白色卡片。
- 文案对访客说话，不出现设计术语；不用感叹号和 emoji；空状态只用文字，不放吉祥物。
- 键盘焦点为 2px 墨色描边、偏移 3px；尊重 `prefers-reduced-motion`。
- 工作区布局：桌面端 72px 左侧导航栏（960px 以下 64px），768px 以下变为 58px 底部导航栏；顶栏 72px（手机 64px），带英文 eyebrow 与衬线标题。

## 已知差异

- 设计系统目前只有浅色主题，所以 Web 界面不再跟随系统切换深色模式。设计系统增加深色主题后，重新生成 `tokens.css` 即可。
- 终端界面在深色背景下使用 `color-on-dark` 系列的近似色（见 `tui.go` 中的注释）。

## 更新设计

1. 在设计系统中修改。
2. 下载新的 `tokens.json`，运行 `python3 scripts/gen-tokens.py <tokens.json>`。
3. 组件样式有变化时，同步 `web/src/style.css` 中对应的类。
4. `make web build`，检查 Web 与终端界面。
