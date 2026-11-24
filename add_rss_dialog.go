package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
	"github.com/simon/youtube-reader/core"
	mui "github.com/simon/youtube-reader/ui"
)

const (
	addRSSDialogWidth              float32 = 420
	addRSSDialogPadding            float32 = 20
	addRSSDialogFieldHeight        float32 = 28
	addRSSDialogInputPadding       float32 = 8
	addRSSDialogGroupFieldHeight   float32 = 26
	addRSSDialogHelperLine         float32 = 14
	addRSSDialogCardHeight         float32 = 52
	addRSSDialogControlHeight      float32 = 28
	addRSSDialogPickerHeight       float32 = 28
	addRSSDialogPickerInset        float32 = 9
	addRSSDialogResolveTimeout             = 12 * time.Second
	addRSSDialogGroupNewSentinel           = "\x00new-group"
)

type addRSSDialogState uint8

const (
	addRSSDialogInitial addRSSDialogState = iota
	addRSSDialogResolving
	addRSSDialogResolved
	addRSSDialogNotFound
	addRSSDialogInvalid
)

type addRSSDialog struct {
	open          bool
	seq           int
	input         string
	resolved      *core.ResolvedRSSSource
	targetGroupID string
	pickerOpen    bool
	newGroupMode  bool
	newGroupName  string
	focusInput    bool
	focusNewGroup bool
	state         addRSSDialogState
	errMessage    string
	cancel        context.CancelFunc
}

type addRSSDialogCopy struct {
	title               string
	fieldLabel          string
	fieldHint           string
	fieldPlaceholder    string
	groupLabel          string
	ungrouped           string
	newGroupItem        string
	newGroupValue       string
	newGroupPlaceholder string
	add                 string
	cancel              string
	resolving           string
	notFound            string
	invalidURL          string
	alreadySubscribed   string
	articleCountSuffix  string
	addedToast          string
	addedUngroupedToast string
	errSave             string
}

func addRSSDialogCopyIn(locale string) addRSSDialogCopy {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return addRSSDialogCopy{
			title:               "添加 RSS 订阅",
			fieldLabel:          "订阅源或网站网址",
			fieldHint:           "支持直接输入 RSS/Atom 源地址，或输入网站主页自动探测。",
			fieldPlaceholder:    "https://example.com/feed.xml 或网站首页",
			groupLabel:          "分组",
			ungrouped:           "未分组",
			newGroupItem:        "新建分组…",
			newGroupValue:       "新分组",
			newGroupPlaceholder: "新分组名称",
			add:                 "添加",
			cancel:              "取消",
			resolving:           "正在探测并解析订阅源…",
			notFound:            "未能在该网址找到有效的 RSS/Atom 订阅源。",
			invalidURL:          "请输入有效的网址（例如 https://example.com/feed.xml）。",
			alreadySubscribed:   "该订阅源已在订阅列表中：%s",
			articleCountSuffix:  "篇可用文章",
			addedToast:          "已添加订阅：%s 到 %s",
			addedUngroupedToast: "已添加订阅：%s",
			errSave:             "保存订阅源失败，请重试。",
		}
	}
	return addRSSDialogCopy{
		title:               "Add RSS Feed",
		fieldLabel:          "Feed or Website URL",
		fieldHint:           "Enter an RSS/Atom/JSON feed URL or a website URL to auto-discover.",
		fieldPlaceholder:    "https://example.com/feed.xml or website homepage",
		groupLabel:          "Group",
		ungrouped:           "Ungrouped",
		newGroupItem:        "New Group…",
		newGroupValue:       "New Group",
		newGroupPlaceholder: "New group name",
		add:                 "Add",
		cancel:              "Cancel",
		resolving:           "Discovering and verifying feed…",
		notFound:            "No RSS/Atom feed found at this URL.",
		invalidURL:          "Please enter a valid URL (e.g. https://example.com/feed.xml).",
		alreadySubscribed:   "Already subscribed: %s",
		articleCountSuffix:  "articles found",
		addedToast:          "Subscribed to %s in %s",
		addedUngroupedToast: "Subscribed to %s",
		errSave:             "Failed to save feed. Please try again.",
	}
}

func (w *Window) openAddRSSDialog() {
	d := &w.addRSSDialog
	if d.open {
		return
	}
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.seq++
	d.open = true
	d.input = ""
	d.resolved = nil
	d.targetGroupID = w.defaultAddRSSGroup()
	d.pickerOpen = false
	d.newGroupMode = false
	d.newGroupName = ""
	d.focusInput = true
	d.focusNewGroup = false
	d.state = addRSSDialogInitial
	d.errMessage = ""
	w.invalidateMainWindow()
}

func (w *Window) closeAddRSSDialog(c *ui.Context) {
	d := &w.addRSSDialog
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.seq++
	d.open = false
	d.pickerOpen = false
	d.newGroupMode = false
	d.state = addRSSDialogInitial
	d.errMessage = ""
	d.resolved = nil
	w.repaintDialog(c)
}

func (w *Window) defaultAddRSSGroup() string {
	if w.app == nil {
		return ""
	}
	sel := w.app.Selected
	if id := destinationID(sel, "group:"); id != "" {
		for _, g := range w.app.RSSGroups {
			if g.ID == id {
				return id
			}
		}
	}
	return ""
}

func (w *Window) beginResolveRSS(c *ui.Context) {
	d := &w.addRSSDialog
	if d.state == addRSSDialogResolving {
		return
	}
	urlInput := strings.TrimSpace(d.input)
	if urlInput == "" {
		d.state = addRSSDialogInvalid
		d.errMessage = ""
		w.repaintDialog(c)
		return
	}

	if d.cancel != nil {
		d.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), addRSSDialogResolveTimeout)
	d.cancel = cancel
	d.state = addRSSDialogResolving
	d.errMessage = ""
	d.resolved = nil
	d.seq++
	seq := d.seq

	go func() {
		res, err := core.ResolveRSSSource(ctx, nil, urlInput)
		mygo.RunOnMain(func() {
			w.applyResolveRSS(seq, res, err)
		})
	}()
	w.repaintDialog(c)
}

func (w *Window) applyResolveRSS(seq int, res *core.ResolvedRSSSource, err error) {
	d := &w.addRSSDialog
	if !d.open || d.seq != seq {
		return
	}
	d.cancel = nil
	if err != nil {
		d.state = addRSSDialogNotFound
		d.errMessage = err.Error()
	} else if res != nil {
		d.state = addRSSDialogResolved
		d.resolved = res
		d.errMessage = ""
	}
	w.invalidateMainWindow()
}

func (w *Window) submitAddRSSDialog(c *ui.Context) {
	d := &w.addRSSDialog
	if d.state == addRSSDialogResolving {
		return
	}
	if d.state != addRSSDialogResolved || d.resolved == nil {
		w.beginResolveRSS(c)
		return
	}

	locale := ""
	if c != nil {
		locale = c.Locale()
	}
	words := addRSSDialogCopyIn(locale)
	feed := d.resolved.Feed
	targetGroupID := d.targetGroupID

	if d.newGroupMode {
		name := strings.TrimSpace(d.newGroupName)
		if name == "" {
			name = words.newGroupValue
		}
		newGroup, err := w.app.CreateRSSGroup(name)
		if err != nil {
			w.toast(c, words.errSave, true)
			return
		}
		targetGroupID = newGroup.ID
	}

	if err := w.app.SubscribeRSSFeed(feed, targetGroupID); err != nil {
		w.toast(c, words.errSave, true)
		return
	}

	if targetGroupID != "" {
		_ = w.app.SetRSSGroupCollapsed(targetGroupID, false)
	}

	w.closeAddRSSDialog(c)

	targetGroupName := words.ungrouped
	if targetGroupID != "" {
		if g := w.app.RSSGroupByID(targetGroupID); g.Name != "" {
			targetGroupName = g.Name
		}
	}

	if targetGroupID == "" {
		w.toast(c, fmt.Sprintf(words.addedUngroupedToast, feed.DisplayName()), false)
	} else {
		w.toast(c, fmt.Sprintf(words.addedToast, feed.DisplayName(), targetGroupName), false)
	}

	// Trigger initial background fetch
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, _ = w.app.FetchRSSFeed(ctx, feed.ID)
		mygo.RunOnMain(func() {
			w.invalidateMainWindow()
		})
	}()
}

// addRSSDialogModal draws the modal dialog surface.
func (w *Window) addRSSDialogModal(c *ui.Context, t mui.Tokens) {
	d := &w.addRSSDialog
	if !d.open {
		return
	}
	words := addRSSDialogCopyIn(c.Locale())
	open := true
	panel := ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(t.ModalScrim)
		panel.Width(addRSSDialogWidth).Shrink(0).Padding(addRSSDialogPadding).
			Radius(mui.RadiusWindow).Background(t.Surface).Label(words.title)
		panel.Shadow(0, mui.ElevationModalOffsetY, mui.ElevationModalBlur, mui.ElevationModalSpread, t.ModalShadow)
		panel.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Stroke(r, t.Border, mui.RadiusWindow, mui.Hairline)
		})
		panel.Children(func() {
			w.addRSSDialogBody(c, t, words)
		})
	})

	panel.OnShortcut(0, ui.KeyEscape, func() {
		if d.pickerOpen {
			d.pickerOpen = false
		} else {
			w.closeAddRSSDialog(c)
		}
		c.Invalidate()
	})
	if panel.Dismissed() {
		w.closeAddRSSDialog(c)
	}
}

func (w *Window) addRSSDialogBody(c *ui.Context, t mui.Tokens, words addRSSDialogCopy) {
	d := &w.addRSSDialog

	mui.DetailTitle.Apply(ui.Text(c, words.title)).TextColor(t.Text).SingleLine().
		Height(20).Margin(0, 0, addDialogGapBlock, 0).Role(ui.RoleHeading).Level(2)

	mui.Secondary.Apply(ui.Text(c, words.fieldLabel)).TextColor(t.Text).SingleLine().
		Height(16).Margin(0, 0, addDialogGapTight, 0)

	w.addRSSDialogLinkField(c, t, words)

	w.addRSSDialogHelper(c, t, words).
		Margin(0, 0, addDialogGapBlock, 0)

	mui.Secondary.Apply(ui.Text(c, words.groupLabel)).TextColor(t.Text).SingleLine().
		Height(16).Margin(0, 0, addDialogGapTight, 0)

	w.addRSSDialogGroupPicker(c, t, words, w.app.RSSGroups).
		Margin(0, 0, addDialogGapControl, 0)

	if d.newGroupMode {
		w.addRSSDialogNewGroupField(c, t, words).
			Margin(0, 0, addDialogGapControl, 0)
	}

	footer := ui.Row(c).Key("add-rss-dialog-footer").Height(addRSSDialogControlHeight).FillWidth().
		Justify(ui.End).AlignItems(ui.Center).Gap(addDialogGapControl).Margin(addDialogGapControl, 0, 0, 0)
	footer.Children(func() {
		w.addRSSDialogCancelButton(c, t, words)
		w.addRSSDialogPrimaryButton(c, t, words)
	})
}

func (w *Window) addRSSDialogLinkField(c *ui.Context, t mui.Tokens, words addRSSDialogCopy) {
	d := &w.addRSSDialog
	border := t.Border
	if d.state == addRSSDialogNotFound || d.state == addRSSDialogInvalid {
		border = t.Danger
	}
	field := ui.TextInput(c.Key("add-rss-dialog-input"), &d.input).Height(addRSSDialogFieldHeight).
		PaddingX(addRSSDialogInputPadding).Radius(mui.RadiusControl).
		Background(t.Surface).Border(mui.Hairline, border).TextColor(t.Text).
		Label(words.fieldLabel).Placeholder(words.fieldPlaceholder).MinWidth(0).
		FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	mui.Control.Apply(field)
	field.Margin(0, 0, addDialogGapTight, 0)
	if d.focusInput {
		d.focusInput = false
		field.Focus()
	}
	field.OnSubmit(func() {
		w.submitAddRSSDialog(c)
	})
	if field.Changed() && d.state != addRSSDialogInitial {
		if d.cancel != nil {
			d.cancel()
			d.cancel = nil
		}
		d.seq++
		d.state = addRSSDialogInitial
		d.errMessage = ""
		d.resolved = nil
		c.Invalidate()
	}
}

func (w *Window) addRSSDialogHelper(c *ui.Context, t mui.Tokens, words addRSSDialogCopy) ui.Element {
	d := &w.addRSSDialog
	switch d.state {
	case addRSSDialogResolved:
		return w.addRSSDialogFeedCard(c, t, words)
	case addRSSDialogResolving:
		row := ui.Row(c).Key("add-rss-dialog-resolving").Height(addRSSDialogHelperLine).AlignItems(ui.Center).Gap(mui.Space(2))
		row.Children(func() {
			ui.Spinner(c).Size(12, 12).TextColor(t.Muted)
			mui.Helper.Apply(ui.Text(c, words.resolving)).TextColor(t.Muted).SingleLine()
		})
		return row
	case addRSSDialogNotFound:
		msg := words.notFound
		if d.errMessage != "" {
			msg = d.errMessage
		}
		return mui.Helper.Apply(ui.Text(c, msg)).TextColor(t.Danger).SingleLine().Height(addRSSDialogHelperLine)
	case addRSSDialogInvalid:
		return mui.Helper.Apply(ui.Text(c, words.invalidURL)).TextColor(t.Danger).SingleLine().Height(addRSSDialogHelperLine)
	default:
		return mui.Helper.Apply(ui.Text(c, words.fieldHint)).TextColor(t.Muted).SingleLine().Height(addRSSDialogHelperLine)
	}
}

func (w *Window) addRSSDialogFeedCard(c *ui.Context, t mui.Tokens, words addRSSDialogCopy) ui.Element {
	d := &w.addRSSDialog
	feed := d.resolved.Feed
	alreadyIn := w.app != nil && w.app.HasRSSFeed(feed.ID)

	card := ui.Row(c).Key("add-rss-dialog-card").Height(addRSSDialogCardHeight).FillWidth().
		Padding(mui.Space(1), mui.Space(2)).Radius(mui.RadiusControl).
		Background(t.SurfaceAlt).Border(mui.Hairline, t.BorderSoft).
		AlignItems(ui.Center).Gap(mui.Space(2))

	card.Children(func() {
		// Feed avatar / monogram
		mui.ChannelAvatar(c, t, feed.AvatarURL, feed.Title, 32)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			mui.Control.Apply(ui.Text(c, feed.DisplayName())).TextColor(t.Text).SingleLine()
			subtitle := fmt.Sprintf("%d %s · %s", d.resolved.ArticleCount, words.articleCountSuffix, feed.FeedURL)
			if alreadyIn {
				subtitle = fmt.Sprintf(words.alreadySubscribed, feed.DisplayName())
			}
			mui.Helper.Apply(ui.Text(c, subtitle)).TextColor(t.Muted).SingleLine()
		})
	})
	return card
}

func (w *Window) addRSSDialogGroupPicker(c *ui.Context, t mui.Tokens, words addRSSDialogCopy, groups []core.RSSGroup) ui.Element {
	d := &w.addRSSDialog
	before := d.targetGroupID
	values := make([]string, 0, len(groups)+2)
	for _, group := range groups {
		values = append(values, group.ID)
	}
	values = append(values, "", addRSSDialogGroupNewSentinel)

	parts := ui.SelectBaseOpen(c, &d.targetGroupID, &d.pickerOpen)
	label := words.ungrouped
	if d.targetGroupID != "" {
		for _, g := range groups {
			if g.ID == d.targetGroupID {
				label = g.Name
				break
			}
		}
	}
	if d.newGroupMode {
		label = words.newGroupValue
	}

	trigger := parts.Trigger.Height(addRSSDialogFieldHeight).FillWidth().Shrink(0).
		PaddingX(addRSSDialogInputPadding).Justify(ui.SpaceBetween).AlignItems(ui.Center).
		Label(label).Radius(mui.RadiusControl).Background(t.Surface).
		Border(mui.Hairline, t.Border).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)

	trigger.Children(func() {
		mui.Control.Apply(ui.Text(c, label)).TextColor(t.Text).SingleLine()
		ui.Icon(c, mui.IconChevronDown).Size(10, 10).TextColor(t.Muted)
	})

	parts.Popup(func(panel ui.Element) {
		panel.MinWidth(addRSSDialogWidth-2*addRSSDialogPadding).Padding(mui.Space(1)).
			Radius(mui.RadiusNotice).Background(t.Surface).Border(mui.Hairline, t.Border).
			Shadow(0, mui.ElevationOffsetY, mui.ElevationBlur, mui.ElevationSpread, t.Shadow)
		for _, value := range values {
			if value == "" {
				ui.Box(c).Height(mui.Hairline).FillWidth().Background(t.BorderSoft).
					Margin(mui.Space(1), 0, mui.Space(1), 0)
			}
			item := parts.Item(value).Height(addRSSDialogPickerHeight).
				Padding(0, addRSSDialogPickerInset).Radius(mui.RadiusControl).
				Justify(ui.Start).AlignItems(ui.Center).Gap(mui.Space(1))
			current := value == d.targetGroupID && !d.newGroupMode
			switch {
			case item.Highlighted():
				item.Background(t.AccentSoft)
			case current:
				item.Background(t.SurfaceAlt)
			}
			name := words.newGroupItem
			switch value {
			case addRSSDialogGroupNewSentinel:
			case "":
				name = words.ungrouped
			default:
				for _, g := range groups {
					if g.ID == value {
						name = g.Name
						break
					}
				}
			}
			item.Children(func() {
				color := t.Text
				if item.Highlighted() {
					color = t.Accent
				}
				slot := ui.Box(c).Size(mui.Space(4), mui.Space(4)).Center()
				slot.Children(func() {
					switch {
					case value == addRSSDialogGroupNewSentinel:
						sfsymbols.Symbol(c, "plus").Size(mui.Space(3)).Color(t.Muted).Element.Role(ui.RoleNone)
					case current:
						sfsymbols.Symbol(c, "checkmark").Size(mui.Space(3)).Color(color).Element.Role(ui.RoleNone)
					}
				})
				mui.Control.Apply(ui.Text(c, name)).TextColor(color).SingleLine()
			})
		}
	})

	if trigger.Changed() {
		if d.targetGroupID == addRSSDialogGroupNewSentinel {
			d.targetGroupID = before
			d.newGroupMode = true
			d.newGroupName = ""
			d.focusNewGroup = true
		} else {
			d.newGroupMode = false
			d.newGroupName = ""
		}
		c.Invalidate()
	}
	return trigger
}

func (w *Window) addRSSDialogNewGroupField(c *ui.Context, t mui.Tokens, words addRSSDialogCopy) ui.Element {
	d := &w.addRSSDialog
	box := ui.Column(c).Key("add-rss-dialog-new-group").Shrink(0)
	box.Children(func() {
		field := ui.TextInput(c.Key("add-rss-dialog-new-group-name"), &d.newGroupName).
			Height(addRSSDialogGroupFieldHeight).PaddingX(addRSSDialogInputPadding).Radius(mui.RadiusControl).
			Background(t.Surface).Border(mui.Hairline, t.Border).TextColor(t.Text).
			Label(words.newGroupPlaceholder).Placeholder(words.newGroupPlaceholder).MinWidth(0).
			FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
		mui.Control.Apply(field)
		if d.focusNewGroup {
			d.focusNewGroup = false
			field.Focus()
		}
		field.OnSubmit(func() {
			w.submitAddRSSDialog(c)
		})
	})
	return box
}

func (w *Window) addRSSDialogCancelButton(c *ui.Context, t mui.Tokens, words addRSSDialogCopy) {
	btn := ui.ButtonBase(c).Key("add-rss-dialog-cancel").Height(addRSSDialogControlHeight).
		PaddingX(mui.Space(3)).Radius(mui.RadiusControl).
		Background(t.Surface).Border(mui.Hairline, t.Border).
		TextColor(t.Text).Label(words.cancel).
		Cursor(ui.CursorPointer).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	mui.Control.Apply(btn)
	btn.Children(func() {
		mui.Control.Apply(ui.Text(c, words.cancel)).TextColor(t.Text).SingleLine()
	})
	btn.OnClick(func() {
		w.closeAddRSSDialog(c)
	})
}

func (w *Window) addRSSDialogPrimaryButton(c *ui.Context, t mui.Tokens, words addRSSDialogCopy) {
	d := &w.addRSSDialog
	disabled := d.state == addRSSDialogResolving
	label := words.add

	btn := ui.ButtonBase(c).Key("add-rss-dialog-primary").Height(addRSSDialogControlHeight).
		PaddingX(mui.Space(3)).Radius(mui.RadiusControl).
		Background(t.Accent).TextColor(t.AccentText).Label(label).
		Cursor(ui.CursorPointer).FocusRingColor(t.Focus).FocusRingOffset(mui.FocusRingGap)
	if disabled {
		btn.Opacity(0.5).Cursor(ui.CursorDefault)
	}
	mui.Control.Apply(btn)
	btn.Children(func() {
		if d.state == addRSSDialogResolving {
			ui.Spinner(c).Size(12, 12).TextColor(t.AccentText)
		} else {
			mui.Control.Apply(ui.Text(c, label)).TextColor(t.AccentText).SingleLine()
		}
	})
	btn.OnClick(func() {
		if !disabled {
			w.submitAddRSSDialog(c)
		}
	})
}
