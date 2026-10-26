package gormstore

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// v10：skill、MCP 插件、牛马助手；用户自己关掉的 skill 与插件、牛马助手用的模型。
// 以下为 v10 的快照结构体，不得修改。

type v10User struct {
	DisabledSkills  string `gorm:"size:2000;not null;default:''"`
	DisabledPlugins string `gorm:"size:2000;not null;default:''"`
	AgentModelID    *uint64
}

func (v10User) TableName() string { return "users" }

type v10Skill struct {
	ID           uint64 `gorm:"primaryKey"`
	Name         string `gorm:"size:64;uniqueIndex;not null"`
	Description  string `gorm:"size:1024;not null;default:''"`
	Source       string `gorm:"size:16;not null;default:''"`
	SourceURL    string `gorm:"size:512;not null;default:''"`
	AllowNetwork bool   `gorm:"not null;default:false"`
	Enabled      bool   `gorm:"not null;default:true"`
	Files        int    `gorm:"not null;default:0"`
	Size         int64  `gorm:"not null;default:0"`
	CreatedAt    ts
	UpdatedAt    ts
}

func (v10Skill) TableName() string { return "skills" }

type v10MCPServer struct {
	ID         uint64 `gorm:"primaryKey"`
	Name       string `gorm:"size:64;uniqueIndex;not null"`
	Transport  string `gorm:"size:16;not null"`
	Command    string `gorm:"size:512;not null;default:''"`
	Args       string `gorm:"size:4000;not null;default:''"`
	EnvEnc     string `gorm:"size:8000;not null;default:''"`
	URL        string `gorm:"size:1024;not null;default:''"`
	HeadersEnc string `gorm:"size:8000;not null;default:''"`
	Enabled    bool   `gorm:"not null;default:true"`
	CreatedAt  ts
	UpdatedAt  ts
}

func (v10MCPServer) TableName() string { return "mcp_servers" }

type v10AgentMessage struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"index;not null"`
	Role      string `gorm:"size:16;not null"`
	Content   string `gorm:"type:text"`
	ToolLog   string `gorm:"type:text"`
	CreatedAt ts
}

func (v10AgentMessage) TableName() string { return "agent_messages" }

type v10AgentAction struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"index;not null"`
	Kind      string `gorm:"size:64;not null"`
	Params    string `gorm:"type:text"`
	Summary   string `gorm:"size:1000;not null;default:''"`
	Admin     bool   `gorm:"not null;default:false"`
	Status    string `gorm:"size:16;not null;default:'pending'"`
	Result    string `gorm:"size:2000;not null;default:''"`
	CreatedAt ts
	UpdatedAt ts
}

func (v10AgentAction) TableName() string { return "agent_actions" }

func migrationV10() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610260010_skills_plugins_agent",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			for _, col := range []string{"DisabledSkills", "DisabledPlugins", "AgentModelID"} {
				if !m.HasColumn(&v10User{}, col) {
					if err := m.AddColumn(&v10User{}, col); err != nil {
						return err
					}
				}
			}
			return autoMigrate(tx, &v10Skill{}, &v10MCPServer{}, &v10AgentMessage{}, &v10AgentAction{})
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if err := m.DropTable(&v10AgentAction{}, &v10AgentMessage{}, &v10MCPServer{}, &v10Skill{}); err != nil {
				return err
			}
			for _, col := range []string{"DisabledSkills", "DisabledPlugins", "AgentModelID"} {
				if m.HasColumn(&v10User{}, col) {
					if err := m.DropColumn(&v10User{}, col); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}
