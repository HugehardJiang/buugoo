package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HugehardJiang/buugoo/internal/llm"
	"github.com/HugehardJiang/buugoo/internal/models"
	"github.com/HugehardJiang/buugoo/internal/quota"
	"github.com/HugehardJiang/buugoo/internal/store"
	"github.com/HugehardJiang/buugoo/internal/uploads"
)

// historyImageLimit 是能看图的模型在历史消息中最多重新看到的图片数（最近的优先）。
const historyImageLimit = 4

// historyDocChars 是历史消息中每个文档保留的字数。
const historyDocChars = 2000

const describePrompt = "请用中文客观、详细地描述这张图片，供另一位不能看图的助手理解：" +
	"包括主体、场景、人物或动物的状态、颜色、图中的所有文字（原样抄录）以及值得注意的细节。不要评价，不要猜测图片之外的事。"

func (s *Service) docChars() int {
	if s.uploads == nil {
		return 20000
	}
	return s.uploads.DocChars()
}

func attachmentIDs(list []store.Attachment) []uint64 {
	ids := make([]uint64, len(list))
	for i, a := range list {
		ids[i] = a.ID
	}
	return ids
}

// pendingAttachments 读取本轮要发送的附件（必须属于该用户且尚未发送）。
func (s *Service) pendingAttachments(ctx context.Context, u *store.User, ids []uint64) ([]store.Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if s.uploads == nil {
		return nil, ErrUploadsDisabled
	}
	if len(ids) > s.uploads.MaxFiles() {
		return nil, fmt.Errorf("%w：一条消息最多 %d 个", ErrTooManyFiles, s.uploads.MaxFiles())
	}
	seen := map[uint64]bool{}
	uniq := ids[:0:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	list, err := s.store.Attachments().ListPending(ctx, u.ID, uniq)
	if err != nil {
		return nil, err
	}
	if len(list) != len(uniq) {
		return nil, ErrAttachmentNotFound
	}
	return list, nil
}

// describeImages 在当前模型不能看图时，用能看图的模型把图片转述成文字（结果缓存在附件上）。
func (s *Service) describeImages(ctx context.Context, u *store.User, model *models.Resolved, atts []store.Attachment) error {
	if model.Vision {
		return nil
	}
	var todo []int
	for i, a := range atts {
		if a.Kind == store.AttachmentImage && a.Description == "" {
			todo = append(todo, i)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	helper, err := s.models.VisionHelper(ctx, modelUser(u))
	if errors.Is(err, models.ErrNoModel) {
		return ErrImagesUnsupported
	}
	if err != nil {
		return err
	}
	for _, i := range todo {
		a := &atts[i]
		url, err := s.uploads.DataURL(a)
		if err != nil {
			return err
		}
		dctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		resp, err := helper.Chat.Chat(dctx, llm.ChatRequest{
			Thinking: "off",
			Messages: []llm.Message{{Role: llm.RoleUser, Content: describePrompt, Images: []llm.Image{{Name: a.Filename, URL: url}}}},
		})
		cancel()
		if err != nil {
			return fmt.Errorf("请「%s」帮忙看图失败：%w", helper.Name, err)
		}
		if !helper.Personal {
			if err := s.quota.Record(ctx, u.ID, quota.Tokens(quota.ModelKey(helper.ID), resp.Usage)); err != nil {
				s.log.Error("记录用量失败", "user_id", u.ID, "err", err)
			}
		}
		a.Description = strings.TrimSpace(resp.Content)
		if err := s.store.Attachments().SetDescription(ctx, a.ID, a.Description); err != nil {
			s.log.Warn("保存图片描述失败", "attachment_id", a.ID, "err", err)
		}
	}
	return nil
}

// renderAttachments 把附件并入消息：文档放入（截断后的）文字，图片在能看图时作为图片发送，
// 否则使用文字描述。返回放进提示词的文字与图片。
func (s *Service) renderAttachments(content string, atts []store.Attachment, vision bool, docChars int) (string, []llm.Image, error) {
	if len(atts) == 0 {
		return content, nil, nil
	}
	var b strings.Builder
	b.WriteString(content)
	var images []llm.Image
	for i := range atts {
		a := &atts[i]
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		if a.DeletedAt != nil {
			// 用户在文件管理中删除了文件：文件内容不再提供，图片保留之前的文字描述。
			if a.Kind == store.AttachmentImage && a.Description != "" {
				fmt.Fprintf(&b, "【图片：%s（用户已经删除了这张图片，以下是之前的描述）】\n%s", a.Filename, a.Description)
			} else {
				fmt.Fprintf(&b, "【文件：%s（用户已经删除了这个文件）】", a.Filename)
			}
			continue
		}
		switch a.Kind {
		case store.AttachmentDocument:
			text, cut := uploads.Truncate(a.Text, docChars)
			fmt.Fprintf(&b, "【文件：%s】\n%s", a.Filename, text)
			if cut {
				b.WriteString("\n…（文件较长，后面的内容已省略）")
			}
			fmt.Fprintf(&b, "\n【文件结束：%s】", a.Filename)
		case store.AttachmentFile:
			fmt.Fprintf(&b, "【文件：%s（%s，读不出文字，你看不到内容；运行代码时可以从 input/%s 读取处理）】", a.Filename, uploads.HumanSize(a.Size), a.Filename)
		case store.AttachmentImage:
			switch {
			case vision && s.uploads != nil:
				url, err := s.uploads.DataURL(a)
				if err != nil {
					return "", nil, err
				}
				images = append(images, llm.Image{Name: a.Filename, URL: url})
				fmt.Fprintf(&b, "【图片：%s】", a.Filename)
			case a.Description != "":
				fmt.Fprintf(&b, "【图片：%s（你看不到图片，以下是别人帮你看的描述）】\n%s", a.Filename, a.Description)
			default:
				fmt.Fprintf(&b, "【图片：%s（无法查看）】", a.Filename)
			}
		}
	}
	return b.String(), images, nil
}

// renderHistory 把历史消息的附件并入内容。能看图时，最近的几张历史图片也一起发送。
func (s *Service) renderHistory(ctx context.Context, u *store.User, history []store.Message, vision bool) ([]store.Message, map[uint64][]llm.Image) {
	byMsg := s.messageAttachments(ctx, u, history)
	if len(byMsg) == 0 {
		return history, nil
	}
	out := make([]store.Message, len(history))
	copy(out, history)
	images := map[uint64][]llm.Image{}
	budget := historyImageLimit
	for i := len(out) - 1; i >= 0; i-- {
		atts := byMsg[out[i].ID]
		if len(atts) == 0 {
			continue
		}
		withImages := vision && budget > 0
		text, imgs, err := s.renderAttachments(out[i].Content, atts, withImages, historyDocChars)
		if err != nil {
			// 图片文件读不到（例如被手动删除）时退回文字说明。
			text, imgs, _ = s.renderAttachments(out[i].Content, atts, false, historyDocChars)
		}
		if len(imgs) > budget {
			imgs = imgs[len(imgs)-budget:]
		}
		budget -= len(imgs)
		out[i].Content = text
		if len(imgs) > 0 {
			images[out[i].ID] = imgs
		}
	}
	return out, images
}

func (s *Service) messageAttachments(ctx context.Context, u *store.User, msgs []store.Message) map[uint64][]store.Attachment {
	return s.attachmentsOf(ctx, u, msgs, false)
}

// attachmentsOf 读取消息的附件。withReplies 为 false 时只读用户消息的（助手回复的附件是代码生成的图片，不放进提示词）。
func (s *Service) attachmentsOf(ctx context.Context, u *store.User, msgs []store.Message, withReplies bool) map[uint64][]store.Attachment {
	var ids []uint64
	for _, m := range msgs {
		if m.Role == store.RoleUser || withReplies {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	list, err := s.store.Attachments().ListByMessages(ctx, u.ID, ids)
	if err != nil {
		s.log.Warn("读取附件失败", "user_id", u.ID, "err", err)
		return nil
	}
	out := map[uint64][]store.Attachment{}
	for _, a := range list {
		if a.MessageID != nil {
			out[*a.MessageID] = append(out[*a.MessageID], a)
		}
	}
	return out
}

// MessageAttachments 返回消息的附件（包括助手回复中代码生成的图片），按消息 ID 索引（用于展示历史消息）。
func (s *Service) MessageAttachments(ctx context.Context, u *store.User, msgs []store.Message) map[uint64][]store.Attachment {
	return s.attachmentsOf(ctx, u, msgs, true)
}

// titleSource 返回用于自动生成对话标题的文字：没有文字时用第一个附件的文件名。
func titleSource(content string, atts []store.Attachment) string {
	if content != "" || len(atts) == 0 {
		return content
	}
	return atts[0].Filename
}
