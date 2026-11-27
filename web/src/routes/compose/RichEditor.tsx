import { useEffect, useState } from 'react';
import { useEditor, EditorContent, type Editor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import Underline from '@tiptap/extension-underline';
import Link from '@tiptap/extension-link';
import Placeholder from '@tiptap/extension-placeholder';
import Image from '@tiptap/extension-image';
import clsx from 'clsx';
import { Icon } from '@/design/icons';
import { Button, Field, Input, Modal } from '@/design';

interface Props {
  value: string;
  onChange: (html: string, text: string) => void;
  onInlineImage: (file: File) => Promise<{ id: string; filename: string } | null>;
  autoFocus?: boolean;
}

export function RichEditor({ value, onChange, onInlineImage, autoFocus }: Props) {
  const [linkDialogOpen, setLinkDialogOpen] = useState(false);
  const [linkUrl, setLinkUrl] = useState('');

  const editor = useEditor({
    extensions: [
      StarterKit.configure({ heading: { levels: [1, 2, 3] } }),
      Underline,
      Link.configure({ openOnClick: false, autolink: true, HTMLAttributes: { rel: 'noopener noreferrer nofollow' } }),
      Image.configure({ inline: true, allowBase64: false }),
      Placeholder.configure({ placeholder: 'Write your message…' }),
    ],
    content: value || '',
    autofocus: autoFocus ? 'end' : false,
    onUpdate: ({ editor }) => {
      const html = editor.isEmpty ? '' : editor.getHTML();
      const text = editor.isEmpty ? '' : editor.getText();
      onChange(html, text);
    },
    editorProps: {
      attributes: {
        class: 'prose prose-sm max-w-none focus:outline-none px-3 py-2 min-h-[240px]',
      },
      handleDrop: (_view, event) => {
        const files = event.dataTransfer?.files;
        if (!files || files.length === 0) return false;
        const images = Array.from(files).filter((f) => f.type.startsWith('image/'));
        if (images.length === 0) return false;
        event.preventDefault();
        void insertInlineImages(images);
        return true;
      },
      handlePaste: (_view, event) => {
        const items = event.clipboardData?.items;
        if (!items) return false;
        const files: File[] = [];
        for (const item of items) {
          if (item.kind === 'file' && item.type.startsWith('image/')) {
            const f = item.getAsFile();
            if (f) files.push(f);
          }
        }
        if (files.length === 0) return false;
        event.preventDefault();
        void insertInlineImages(files);
        return true;
      },
    },
  });

  // Sync external value when it changes out from under us (e.g. quoted body
  // loaded after the editor mounts).
  useEffect(() => {
    if (!editor) return;
    const current = editor.isEmpty ? '' : editor.getHTML();
    if (value !== current) {
      editor.commands.setContent(value || '', false);
    }
  }, [value, editor]);

  async function insertInlineImages(files: File[]) {
    if (!editor) return;
    for (const file of files) {
      const uploaded = await onInlineImage(file);
      if (!uploaded) continue;
      const node = document.createElement('img');
      node.src = `cid:${uploaded.id}`;
      node.alt = uploaded.filename;
      editor.chain().focus().insertContent(node.outerHTML).run();
    }
  }

  if (!editor) return null;

  return (
    <div className="flex flex-col flex-1 min-h-0">
      <Toolbar
        editor={editor}
        onLink={() => {
          const prev = editor.getAttributes('link').href as string | undefined;
          setLinkUrl(prev ?? '');
          setLinkDialogOpen(true);
        }}
      />
      <div className="flex-1 overflow-y-auto scroll-thin">
        <EditorContent editor={editor} />
      </div>

      <Modal
        open={linkDialogOpen}
        onClose={() => setLinkDialogOpen(false)}
        title="Insert link"
        footer={
          <>
            <Button variant="secondary" onClick={() => setLinkDialogOpen(false)}>Cancel</Button>
            <Button
              variant="primary"
              onClick={() => {
                const url = linkUrl.trim();
                if (!url) { editor.chain().focus().unsetLink().run(); setLinkDialogOpen(false); return; }
                const normalized = /^[a-z][a-z0-9+.-]*:/i.test(url) ? url : `https://${url}`;
                editor.chain().focus().extendMarkRange('link').setLink({ href: normalized }).run();
                setLinkDialogOpen(false);
              }}
            >Apply</Button>
          </>
        }
      >
        <Field label="Link URL" htmlFor="link-url">
          <Input id="link-url" autoFocus value={linkUrl} onChange={(e) => setLinkUrl(e.target.value)} placeholder="https://" />
        </Field>
      </Modal>
    </div>
  );
}

function Toolbar({ editor, onLink }: { editor: Editor; onLink: () => void }) {
  return (
    <div className="flex items-center gap-0.5 px-2 py-1 border-b border-line flex-wrap">
      <TB onClick={() => editor.chain().focus().toggleBold().run()} active={editor.isActive('bold')} title="Bold">
        <strong>B</strong>
      </TB>
      <TB onClick={() => editor.chain().focus().toggleItalic().run()} active={editor.isActive('italic')} title="Italic">
        <em>I</em>
      </TB>
      <TB onClick={() => editor.chain().focus().toggleUnderline().run()} active={editor.isActive('underline')} title="Underline">
        <u>U</u>
      </TB>
      <div className="w-px h-4 bg-line mx-1" aria-hidden />
      <TB onClick={() => editor.chain().focus().toggleBulletList().run()} active={editor.isActive('bulletList')} title="Bulleted list">
        <Icon name="inbox" size={14} />
      </TB>
      <TB onClick={() => editor.chain().focus().toggleOrderedList().run()} active={editor.isActive('orderedList')} title="Numbered list">
        <span className="text-xs font-mono">1.</span>
      </TB>
      <TB onClick={() => editor.chain().focus().toggleBlockquote().run()} active={editor.isActive('blockquote')} title="Quote">
        <span className="text-xs">"</span>
      </TB>
      <div className="w-px h-4 bg-line mx-1" aria-hidden />
      <TB onClick={onLink} active={editor.isActive('link')} title="Insert link">
        <Icon name="external-link" size={14} />
      </TB>
      <div className="flex-1" />
      <TB onClick={() => editor.chain().focus().undo().run()} title="Undo" disabled={!editor.can().undo()}>
        <span className="text-xs">↶</span>
      </TB>
      <TB onClick={() => editor.chain().focus().redo().run()} title="Redo" disabled={!editor.can().redo()}>
        <span className="text-xs">↷</span>
      </TB>
    </div>
  );
}

function TB({
  onClick, active, title, children, disabled,
}: { onClick: () => void; active?: boolean; title: string; children: React.ReactNode; disabled?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      aria-label={title}
      aria-pressed={active}
      disabled={disabled}
      className={clsx(
        'px-1.5 py-1 rounded text-sm hover:bg-canvas disabled:opacity-40 disabled:cursor-not-allowed',
        active ? 'bg-canvas text-accent' : 'text-ink-soft',
      )}
    >
      {children}
    </button>
  );
}
