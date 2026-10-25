// Package channels 把布谷接到聊天软件里：个人微信（ClawBot 接口）、QQ（NapCat 等 OneBot v11 实现）、Telegram。
// 只处理私聊。聊天软件里的人用绑定码绑定到布谷的账号后，和网页共用同一份记忆、心情与对话。
// 接入方式参考了 AstrBot（https://github.com/AstrBotDevs/AstrBot）的实现。
package channels

import (
	"context"
	"time"
)

// 聊天软件。
const (
	Weixin   = "weixin"
	QQ       = "qq"
	Telegram = "telegram"
)

// Labels 是聊天软件的中文名。
var Labels = map[string]string{Weixin: "微信", QQ: "QQ", Telegram: "Telegram"}

// Image 是收发的一张图片。
type Image struct {
	Name string
	Data []byte
}

// Inbound 是收到的一条私聊消息。
type Inbound struct {
	Channel  string
	PeerID   string
	PeerName string
	Text     string
	Images   []Image
	At       time.Time
}

// Outbound 是要发出的一条消息：文字或图片。
type Outbound struct {
	Text  string
	Image *Image
}

// Handler 处理收到的消息。
type Handler func(ctx context.Context, in Inbound)

// Status 是聊天软件的连接状态（管理后台显示）。
type Status struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Connected bool   `json:"connected"`
	// Account 是登录的账号（微信 ID、QQ 号、机器人用户名）。
	Account string `json:"account,omitempty"`
	// Detail 是给管理员看的说明（例如需要扫码登录、等待 NapCat 连接）。
	Detail string `json:"detail,omitempty"`
}

// Channel 是一个聊天软件的接入。
type Channel interface {
	Name() string
	// Run 收消息直到 ctx 取消，收到的私聊消息交给 h。连接断开时自行重连。
	Run(ctx context.Context, h Handler)
	Send(ctx context.Context, peer string, out Outbound) error
	// Typing 显示「对方正在输入」，返回的函数结束显示。不支持时什么也不做。
	Typing(ctx context.Context, peer string) (stop func())
	Status() Status
}
