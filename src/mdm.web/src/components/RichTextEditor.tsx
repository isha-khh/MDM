import { useEffect, useRef } from "react";
import { useEditor, EditorContent } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Image from "@tiptap/extension-image";
import { Bold, Italic, List, ListOrdered, Image as ImageIcon } from "lucide-react";

interface RichTextEditorProps {
  value: string;
  onChange: (html: string) => void;
  onUploadImage: (file: File) => Promise<string>;
}

// A small "webmail-style" rich text editor — bold/italic/lists plus an
// insert-image button that uploads the file and drops it inline. Value is
// plain HTML in both directions (Tiptap's getHTML()/setContent()).
export function RichTextEditor({ value, onChange, onUploadImage }: RichTextEditorProps) {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const editor = useEditor({
    extensions: [StarterKit, Image],
    content: value,
    onUpdate: ({ editor }) => onChange(editor.getHTML()),
    editorProps: {
      attributes: { class: "text-sm min-h-32 max-h-96 overflow-y-auto p-3 focus:outline-none [&_img]:max-w-full [&_img]:rounded" },
    },
  });

  // Re-sync when `value` changes from outside this component (e.g. the
  // parent loads a different category's saved content into the same editor
  // instance) — comparing against editor.getHTML() avoids clobbering the
  // user's own in-progress edits on every keystroke, since onUpdate above
  // already keeps `value` equal to the editor's own content as they type.
  useEffect(() => {
    if (editor && value !== editor.getHTML()) {
      editor.commands.setContent(value || "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value, editor]);

  const handleFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file || !editor) return;
    try {
      const url = await onUploadImage(file);
      editor.chain().focus().setImage({ src: url }).run();
    } catch { /* best-effort — user can just try inserting again */ }
  };

  if (!editor) return null;

  return (
    <div className="border border-base-300 rounded-lg overflow-hidden">
      <div className="flex items-center gap-1 border-b border-base-300 bg-base-200 p-1">
        <button
          type="button"
          className={`btn btn-ghost btn-xs btn-square ${editor.isActive("bold") ? "btn-active" : ""}`}
          onClick={() => editor.chain().focus().toggleBold().run()}
        >
          <Bold size={14} />
        </button>
        <button
          type="button"
          className={`btn btn-ghost btn-xs btn-square ${editor.isActive("italic") ? "btn-active" : ""}`}
          onClick={() => editor.chain().focus().toggleItalic().run()}
        >
          <Italic size={14} />
        </button>
        <button
          type="button"
          className={`btn btn-ghost btn-xs btn-square ${editor.isActive("bulletList") ? "btn-active" : ""}`}
          onClick={() => editor.chain().focus().toggleBulletList().run()}
        >
          <List size={14} />
        </button>
        <button
          type="button"
          className={`btn btn-ghost btn-xs btn-square ${editor.isActive("orderedList") ? "btn-active" : ""}`}
          onClick={() => editor.chain().focus().toggleOrderedList().run()}
        >
          <ListOrdered size={14} />
        </button>
        <div className="w-px h-4 bg-base-300 mx-1" />
        <button type="button" className="btn btn-ghost btn-xs btn-square" title="插入圖片" onClick={() => fileInputRef.current?.click()}>
          <ImageIcon size={14} />
        </button>
        <input ref={fileInputRef} type="file" accept="image/*" className="hidden" onChange={handleFile} />
      </div>
      <EditorContent editor={editor} />
    </div>
  );
}
