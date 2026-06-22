package templates

import (
	"context"
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/a-h/templ"

	mw "whatsapptool/internal/web/middleware"
)

// TemplateNewPage renders the full-page "New Template" creation form.
func TemplateNewPage(
	agent *mw.AgentClaims,
	prefillName, prefillCategory, prefillLanguage, prefillBody, errMsg string,
) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(agent, "/templates", "New Template", "")); err != nil {
			return err
		}

		if prefillCategory == "" {
			prefillCategory = "marketing"
		}
		if prefillLanguage == "" {
			prefillLanguage = "en"
		}
		slug := tnSlugify(prefillName)

		if _, err := fmt.Fprintf(w, `<div class="page-wrap" x-data="%s">`,
			tnXData(prefillName, slug, prefillCategory, prefillLanguage, prefillBody),
		); err != nil {
			return err
		}

		// Page header
		if _, err := io.WriteString(w, `
<div class="tn-page-hd">
  <a class="tn-back" href="/templates">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" width="14" height="14"><polyline points="15 18 9 12 15 6"/></svg>
  </a>
  <div>
    <h1 class="screen-title">New Template</h1>
    <p class="screen-subtitle">Create a WhatsApp message template for campaigns and automation</p>
  </div>
</div>`); err != nil {
			return err
		}

		if errMsg != "" {
			fmt.Fprintf(w, `<div class="form-banner form-banner--error">%s</div>`, html.EscapeString(errMsg))
		}

		// Form — multipart so files can be uploaded
		if _, err := io.WriteString(w, `
<form method="post" action="/templates/new" enctype="multipart/form-data">
<div class="tmpl-fp-grid">
<div class="tn-left-col">`); err != nil {
			return err
		}

		// ── BASIC INFO CARD ──────────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="card-static">
  <div class="tmpl-fp-section" style="border-bottom:none">
    <div class="tn-card-title">Basic info</div>
    <div class="tn-two-col">
      <div class="form-group" style="margin:0">
        <label class="form-label">Template name <span class="req">*</span></label>
        <input class="form-input" type="text" name="name" x-model="name" @input="updateSlug()" placeholder="e.g. Diwali Sale Promo" required autocomplete="off">
      </div>
      <div class="form-group" style="margin:0">
        <label class="form-label">Template ID <span class="tn-label-hint">auto-generated</span></label>
        <div class="tn-slug-row" x-text="slug||'template_id'"></div>
      </div>
    </div>
    <div class="tn-two-col">
      <div class="form-group" style="margin:0">
        <label class="form-label">Category</label>
        <select class="form-input" name="category" x-model="category">`); err != nil {
			return err
		}

		for _, o := range []struct{ v, l string }{
			{"marketing", "Marketing"}, {"utility", "Utility"}, {"authentication", "Authentication"},
		} {
			sel := ""
			if o.v == prefillCategory {
				sel = " selected"
			}
			fmt.Fprintf(w, `<option value="%s"%s>%s</option>`, o.v, sel, o.l)
		}

		if _, err := io.WriteString(w, `
        </select>
      </div>
      <div class="form-group" style="margin:0">
        <label class="form-label">Language</label>
        <select class="form-input" name="language" x-model="language">`); err != nil {
			return err
		}

		for _, o := range []struct{ v, l string }{
			{"en", "English"}, {"en_US", "English (US)"}, {"en_GB", "English (GB)"},
			{"hi", "Hindi"}, {"mr", "Marathi"}, {"ta", "Tamil"},
		} {
			sel := ""
			if o.v == prefillLanguage {
				sel = " selected"
			}
			fmt.Fprintf(w, `<option value="%s"%s>%s</option>`, o.v, sel, o.l)
		}

		if _, err := io.WriteString(w, `
        </select>
      </div>
    </div>
  </div>
</div>`); err != nil {
			return err
		}

		// ── MESSAGE CONTENT CARD ─────────────────────────────────────────────
		if _, err := fmt.Fprintf(w, `
<div class="card-static">
  <div class="tmpl-fp-section" style="border-bottom:none">
    <div class="tn-card-title">Message content</div>

    <div class="form-group" style="margin:0">
      <label class="form-label">Header <span class="tn-label-hint">(optional)</span></label>
      <div class="tn-hdr-pills">
        <label class="tn-hdr-pill" :class="{'tn-hdr-pill--sel':headerType==='none'}">
          <input type="radio" name="header_type" value="none" x-model="headerType" @change="clearHeaderFile()" class="sr-only">None
        </label>
        <label class="tn-hdr-pill" :class="{'tn-hdr-pill--sel':headerType==='text'}">
          <input type="radio" name="header_type" value="text" x-model="headerType" @change="clearHeaderFile()" class="sr-only">Text
        </label>
        <label class="tn-hdr-pill" :class="{'tn-hdr-pill--sel':headerType==='image'}">
          <input type="radio" name="header_type" value="image" x-model="headerType" @change="clearHeaderFile()" class="sr-only">Image
        </label>
        <label class="tn-hdr-pill" :class="{'tn-hdr-pill--sel':headerType==='video'}">
          <input type="radio" name="header_type" value="video" x-model="headerType" @change="clearHeaderFile()" class="sr-only">Video
        </label>
        <label class="tn-hdr-pill" :class="{'tn-hdr-pill--sel':headerType==='document'}">
          <input type="radio" name="header_type" value="document" x-model="headerType" @change="clearHeaderFile()" class="sr-only">Document
        </label>
      </div>

      <!-- Text header input -->
      <div x-show="headerType==='text'" x-cloak style="margin-top:8px">
        <input type="text" name="header_text" x-model="headerText" class="form-input"
          placeholder="Header text (max 60 chars)" maxlength="60"
          :disabled="headerType!=='text'">
      </div>

      <!-- Media upload area -->
      <div x-show="headerType==='image'||headerType==='video'||headerType==='document'" x-cloak style="margin-top:10px">
        <div class="tn-upload-area"
          :class="{'tn-upload-area--has': headerFileName}"
          @click="$refs.hfile.click()"
          @dragover.prevent
          @drop.prevent="handleFileDrop($event)">
          <!-- Hidden file input — accept changes based on header type -->
          <input type="file" x-ref="hfile" name="header_file"
            :accept="headerType==='image'?'image/jpeg,image/png,image/webp':headerType==='video'?'video/mp4,video/3gpp':'application/pdf'"
            :disabled="headerType==='none'||headerType==='text'"
            @change="previewHeaderFile($event)"
            class="sr-only">

          <!-- Empty state -->
          <div x-show="!headerFileName">
            <svg class="tn-upload-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" width="32" height="32">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
              <polyline points="17 8 12 3 7 8"/>
              <line x1="12" y1="3" x2="12" y2="15"/>
            </svg>
            <p class="tn-upload-cta">Click to upload or drag &amp; drop</p>
            <p class="tn-upload-hint"
              x-text="headerType==='image'?'PNG, JPG, WebP — max 5 MB':headerType==='video'?'MP4, 3GP — max 16 MB':'PDF — max 100 MB'"></p>
          </div>

          <!-- Image preview -->
          <div x-show="headerFileName && headerType==='image'">
            <img :src="headerPreviewURL" class="tn-upload-thumb" alt="preview">
            <p class="tn-upload-name" x-text="headerFileName"></p>
            <p class="tn-upload-change">Click to change</p>
          </div>

          <!-- Video / document preview -->
          <div x-show="headerFileName && headerType!=='image'">
            <svg class="tn-upload-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" width="28" height="28">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
              <polyline points="14 2 14 8 20 8"/>
            </svg>
            <p class="tn-upload-name" x-text="headerFileName"></p>
            <p class="tn-upload-change">Click to change</p>
          </div>
        </div>
      </div>
    </div>

    <div class="form-group" style="margin:0">
      <label class="form-label">Body <span class="req">*</span></label>
      <textarea id="tmpl-body-ta" class="form-input" name="body" x-model="body" rows="6" required
        placeholder="Hi {{1}}, your order *#{{2}}* has been confirmed!&#10;&#10;Use *bold*, _italic_. Add variables with {{1}}, {{2}}...">%s</textarea>
      <div class="tn-body-toolbar">
        <button type="button" class="tn-tool-btn" @click="insert('*bold*')">*bold*</button>
        <button type="button" class="tn-tool-btn" @click="insert('_italic_')">_italic_</button>
        <button type="button" class="tn-tool-btn" @click="insert('{{1}}')">{{1}}</button>
        <button type="button" class="tn-tool-btn" @click="insert('{{2}}')">{{2}}</button>
        <span class="tn-counter" x-text="varCount+' variables · '+charCount+' chars'">0 variables · 0 chars</span>
      </div>
    </div>

    <div class="form-group" style="margin:0">
      <label class="form-label">Footer <span class="tn-label-hint">(optional)</span></label>
      <input type="text" name="footer" x-model="footer" class="form-input" placeholder="Reply STOP to opt out" maxlength="60">
    </div>
  </div>
</div>`, html.EscapeString(prefillBody)); err != nil {
			return err
		}

		// ── BUTTONS CARD ─────────────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="card-static">
  <div class="tmpl-fp-section" style="border-bottom:none">
    <div style="display:flex;align-items:center;justify-content:space-between">
      <span class="tn-card-title" style="margin-bottom:0">Buttons <span style="font-size:12px;font-weight:400;color:var(--text-muted)">up to 3</span></span>
      <button type="button" class="btn btn-secondary btn-sm" @click="addButton()" x-show="buttons.length<3">+ Add button</button>
    </div>
    <template x-for="(btn,i) in buttons" :key="i">
      <div class="tmpl-btn-row" style="margin-top:8px">
        <select :name="'btn_type_'+i" x-model="btn.type" class="form-input form-input-sm" style="width:140px;flex-shrink:0">
          <option value="QUICK_REPLY">Quick reply</option>
          <option value="URL">URL</option>
          <option value="PHONE_NUMBER">Call</option>
        </select>
        <input :name="'btn_label_'+i" x-model="btn.label" type="text" class="form-input" placeholder="Button label" style="flex:1;min-width:0">
        <button type="button" @click="removeButton(i)" title="Remove" class="tn-del-btn">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="15" height="15"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4h6v2"/></svg>
        </button>
      </div>
    </template>
    <p x-show="buttons.length===0" x-cloak style="color:var(--text-muted);font-size:13px;margin:8px 0 0">No buttons added. Buttons let contacts quickly reply or take action.</p>
  </div>
</div>
</div><!-- tn-left-col -->`); err != nil {
			return err
		}

		// ── RIGHT: PREVIEW SIDEBAR ────────────────────────────────────────────
		if _, err := io.WriteString(w, `
<div class="tmpl-fp-preview">
  <div class="tmpl-fp-preview-card">
    <div class="tmpl-fp-preview-title">PREVIEW</div>
    <div class="tn-wa-preview">
      <div class="tn-wa-hd">
        <div class="tn-wa-av">B</div>
        <div class="tn-wa-meta">
          <div class="tn-wa-biz">Your Business</div>
          <div class="tn-wa-sub">Business Account</div>
        </div>
      </div>
      <div class="tn-wa-body">
        <!-- Header previews -->
        <div x-show="headerType==='text'&&headerText" x-cloak class="tn-wa-hdr-text" x-text="headerText"></div>
        <div x-show="headerType==='image'&&headerPreviewURL" x-cloak class="tn-wa-hdr-img">
          <img :src="headerPreviewURL" alt="header" style="width:100%;border-radius:6px 6px 0 0;display:block;max-height:140px;object-fit:cover">
        </div>
        <div x-show="headerType==='image'&&!headerPreviewURL" x-cloak class="tn-wa-hdr-media">📷 Image</div>
        <div x-show="headerType==='video'" x-cloak class="tn-wa-hdr-media">🎥 <span x-text="headerFileName||'Video'"></span></div>
        <div x-show="headerType==='document'" x-cloak class="tn-wa-hdr-media">📄 <span x-text="headerFileName||'Document'"></span></div>
        <div class="tn-wa-bub">
          <div class="preview-body-text" x-html="renderBody(body)"></div>
          <div x-show="footer" x-cloak class="preview-footer-text" x-text="footer"></div>
          <div class="tn-wa-ts">10:24 AM ✓✓</div>
        </div>
        <template x-for="btn in buttons" :key="btn.label">
          <div class="preview-btn" x-text="btn.label||'Button'"></div>
        </template>
      </div>
    </div>
  </div>
  <div class="tn-meta-note">Templates must be approved by Meta before use. Marketing templates typically take 2–5 minutes.</div>
</div>`); err != nil {
			return err
		}

		// Close grid + form footer
		if _, err := io.WriteString(w, `
</div><!-- tmpl-fp-grid -->
<div class="tn-form-footer">
  <a href="/templates" class="btn btn-secondary">Cancel</a>
  <div style="display:flex;gap:8px">
    <button name="action" value="draft" type="submit" class="btn btn-secondary">Save as draft</button>
    <button name="action" value="submit" type="submit" class="btn btn-primary" :disabled="!body.trim()" :class="{'btn-disabled':!body.trim()}">Submit for approval</button>
  </div>
</div>
</form>
</div><!-- page-wrap -->`); err != nil {
			return err
		}

		_, err := io.WriteString(w, ShellClose())
		return err
	})
}

// tnXData builds the Alpine x-data attribute value for the template form.
func tnXData(name, slug, category, language, body string) string {
	prefix := fmt.Sprintf(
		`{name:%s,slug:%s,category:%s,language:%s,headerType:'none',headerText:'',body:%s,footer:'',buttons:[],`+
			`headerFileName:'',headerPreviewURL:'',`,
		jsLit(name), jsLit(slug), jsLit(category), jsLit(language), jsLit(body),
	)
	suffix := `` +
		`get varCount(){return(this.body.match(/\{\{\d+\}\}/g)||[]).length},` +
		`get charCount(){return this.body.length},` +
		`updateSlug(){this.slug=this.name.toLowerCase().replace(/[^a-z0-9]+/g,'_').replace(/^_+|_+$/g,'')},` +
		`addButton(){if(this.buttons.length<3)this.buttons.push({type:'QUICK_REPLY',label:'',url:'',phone:''})},` +
		`removeButton(i){this.buttons.splice(i,1)},` +
		`clearHeaderFile(){this.headerFileName='';this.headerPreviewURL='';const el=this.$refs.hfile;if(el)el.value=''},` +
		`previewHeaderFile(e){` +
		`const f=e.target.files[0];if(!f)return;` +
		`this.headerFileName=f.name;` +
		`if(this.headerType==='image'){` +
		`const r=new FileReader();r.onload=(ev)=>{this.headerPreviewURL=ev.target.result};r.readAsDataURL(f)` +
		`}},` +
		`handleFileDrop(e){` +
		`const f=e.dataTransfer.files[0];if(!f)return;` +
		`const dt=new DataTransfer();dt.items.add(f);` +
		`const el=this.$refs.hfile;if(el){el.files=dt.files;el.dispatchEvent(new Event('change'))}},` +
		`insert(txt){const ta=document.getElementById('tmpl-body-ta');if(!ta)return;` +
		`const s=ta.selectionStart,e=ta.selectionEnd;` +
		`this.body=this.body.slice(0,s)+txt+this.body.slice(e);` +
		`this.$nextTick(()=>{ta.selectionStart=ta.selectionEnd=s+txt.length;ta.focus()})},` +
		`renderBody(t){if(!t)return'<span style="color:var(--text-muted)">Your message body...</span>';` +
		`t=t.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');` +
		`t=t.replace(/\*([^*\n]+)\*/g,'<strong>$1</strong>');` +
		`t=t.replace(/_([^_\n]+)_/g,'<em>$1</em>');` +
		`return t.replace(/\n/g,'<br>')}}`
	return html.EscapeString(prefix + suffix)
}

func jsLit(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", ``)
	return "'" + s + "'"
}

func tnSlugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
		} else if !prevUnderscore && b.Len() > 0 {
			b.WriteRune('_')
			prevUnderscore = true
		}
	}
	return strings.TrimRight(b.String(), "_")
}
