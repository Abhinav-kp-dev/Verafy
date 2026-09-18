import { useMemo, useState } from 'react'

/** Checkbox-selection state for a list of ids, reset whenever the underlying id set changes. */
export function useSelection(currentIds: string[]) {
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const visible = useMemo(() => new Set(currentIds), [currentIds])

  const toggle = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }
  const selectedVisible = [...selected].filter((id) => visible.has(id))
  const allSelected = currentIds.length > 0 && selectedVisible.length === currentIds.length
  const toggleAll = () => setSelected(allSelected ? new Set() : new Set(currentIds))
  const clear = () => setSelected(new Set())

  return { selected: new Set(selectedVisible), count: selectedVisible.length, toggle, toggleAll, allSelected, clear }
}
