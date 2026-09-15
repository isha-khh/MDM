import { useMemo } from "react";

interface Category {
  id: string;
  parent_id: string | null;
  name: string;
  level: number;
}

interface CategoryLeafSelectProps {
  categories: Category[];
  value: string;
  onChange: (id: string) => void;
  placeholder?: string;
  className?: string;
  disabled?: boolean;
}

/**
 * A single <select> that displays categories in tree order with indentation.
 * Only leaf nodes (categories with no children) are selectable;
 * parent nodes are shown as visual group headers (disabled).
 */
export function CategoryLeafSelect({
  categories,
  value,
  onChange,
  placeholder = "選擇分類",
  className = "select select-bordered select-sm",
  disabled = false,
}: CategoryLeafSelectProps) {
  // Collect all IDs that appear as parent_id of another category → non-leaf
  const parentIds = useMemo(() => {
    const s = new Set<string>();
    for (const c of categories) {
      if (c.parent_id) s.add(c.parent_id);
    }
    return s;
  }, [categories]);

  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={className}
      disabled={disabled}
    >
      <option value="">{placeholder}</option>
      {categories.map((c) => {
        const isParent = parentIds.has(c.id);
        const indent = "    ".repeat(c.level);
        const prefix = c.level > 0 ? "└ " : "";
        return (
          <option
            key={c.id}
            value={c.id}
            disabled={isParent}
            style={isParent ? { color: "gray", fontWeight: 600 } : undefined}
          >
            {indent}{prefix}{c.name}
          </option>
        );
      })}
    </select>
  );
}
