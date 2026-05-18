import { ref } from 'vue'

export type KanbanColumnKey = 'pending' | 'inProgress' | 'completed'

export interface KanbanCard {
  /** Unique identifier for the card: `${sessionId}-${taskId}` */
  id: string
  /** Human-readable label used in screen-reader announcements */
  label: string
}

export interface MovePayload {
  cardId: string
  fromColumn: KanbanColumnKey
  toColumn: KanbanColumnKey
}

const COLUMN_ORDER: KanbanColumnKey[] = ['pending', 'inProgress', 'completed']

const COLUMN_LABELS: Record<KanbanColumnKey, string> = {
  pending: 'Pending',
  inProgress: 'In Progress',
  completed: 'Completed',
}

/**
 * Manages keyboard pick-up / move / drop state for KanbanBoard.
 *
 * Usage:
 *   const kb = useKanbanKeyboard(onCommit)
 *   - Bind kb.handleKeydown to @keydown on each card element
 *   - Bind :aria-grabbed to kb.isPickedUp(cardId)
 *   - Bind :class with kb.isPickedUp(cardId) for the ring highlight
 *   - Render kb.announcement inside an aria-live="assertive" region
 *   - Provide the display column override via kb.displayColumn(cardId, originalColumnKey)
 *
 * @param onCommit  Called when the user presses Enter/Space to drop the card.
 *                  Receives the card id, the source column, and the target column.
 *                  The composable updates local state immediately (optimistic);
 *                  callers may revert on API error.
 */
export function useKanbanKeyboard(onCommit: (payload: MovePayload) => void) {
  const pickedUpCardId = ref<string | null>(null)
  const originalColumn = ref<KanbanColumnKey | null>(null)
  const currentColumn = ref<KanbanColumnKey | null>(null)
  const announcement = ref('')

  function isPickedUp(cardId: string): boolean {
    return pickedUpCardId.value === cardId
  }

  /**
   * Returns the column key to display the card in, taking move-mode into account.
   * When a card is picked up and moved, it should appear in `currentColumn`
   * rather than its original column.
   */
  function displayColumn(cardId: string, originalColKey: KanbanColumnKey): KanbanColumnKey {
    if (pickedUpCardId.value === cardId && currentColumn.value !== null) {
      return currentColumn.value
    }
    return originalColKey
  }

  function pickUp(cardId: string, colKey: KanbanColumnKey, cardLabel: string) {
    pickedUpCardId.value = cardId
    originalColumn.value = colKey
    currentColumn.value = colKey
    announcement.value
      = `${cardLabel} picked up. Use left and right arrow keys to move between columns. Press Enter or Space to drop, Escape to cancel.`
  }

  function cancelMove(refocusEl?: HTMLElement | null) {
    if (!pickedUpCardId.value)
      return
    currentColumn.value = originalColumn.value
    announcement.value = 'Move cancelled.'
    pickedUpCardId.value = null
    originalColumn.value = null
    currentColumn.value = null
    refocusEl?.focus()
  }

  function commitMove(refocusEl?: HTMLElement | null) {
    if (!pickedUpCardId.value || !originalColumn.value || !currentColumn.value)
      return

    const payload: MovePayload = {
      cardId: pickedUpCardId.value,
      fromColumn: originalColumn.value,
      toColumn: currentColumn.value,
    }

    onCommit(payload)
    announcement.value = `Card dropped in ${COLUMN_LABELS[payload.toColumn]}.`

    pickedUpCardId.value = null
    originalColumn.value = null
    currentColumn.value = null
    refocusEl?.focus()
  }

  function moveLeft(cardLabel: string) {
    if (!currentColumn.value)
      return
    const idx = COLUMN_ORDER.indexOf(currentColumn.value)
    if (idx <= 0) {
      announcement.value = `Already in the first column, ${COLUMN_LABELS[COLUMN_ORDER[0]]}.`
      return
    }
    currentColumn.value = COLUMN_ORDER[idx - 1]
    announcement.value = `${cardLabel} moved to ${COLUMN_LABELS[currentColumn.value]} column.`
  }

  function moveRight(cardLabel: string) {
    if (!currentColumn.value)
      return
    const idx = COLUMN_ORDER.indexOf(currentColumn.value)
    if (idx >= COLUMN_ORDER.length - 1) {
      announcement.value = `Already in the last column, ${COLUMN_LABELS[COLUMN_ORDER[COLUMN_ORDER.length - 1]]}.`
      return
    }
    currentColumn.value = COLUMN_ORDER[idx + 1]
    announcement.value = `${cardLabel} moved to ${COLUMN_LABELS[currentColumn.value]} column.`
  }

  /**
   * Main keydown handler. Wire this to the @keydown event on each kanban card.
   *
   * @param event       The keyboard event
   * @param cardId      Stable id for this card (`${sessionId}-${taskId}`)
   * @param colKey      The column this card currently occupies in the data model
   * @param cardLabel   Human-readable label for screen-reader announcements
   * @param refocusEl   Element to return focus to after drop/cancel (usually the card element itself)
   */
  function handleKeydown(
    event: KeyboardEvent,
    cardId: string,
    colKey: KanbanColumnKey,
    cardLabel: string,
    refocusEl?: HTMLElement | null,
  ) {
    const inMoveMode = pickedUpCardId.value === cardId

    if (inMoveMode) {
      switch (event.key) {
        case 'ArrowLeft':
          event.preventDefault()
          moveLeft(cardLabel)
          break
        case 'ArrowRight':
          event.preventDefault()
          moveRight(cardLabel)
          break
        case 'Enter':
        case ' ':
          event.preventDefault()
          commitMove(refocusEl)
          break
        case 'Escape':
          event.preventDefault()
          cancelMove(refocusEl)
          break
      }
    }
    else {
      // Not in move mode — Enter/Space initiates pick-up
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault()
        pickUp(cardId, colKey, cardLabel)
      }
    }
  }

  return {
    pickedUpCardId,
    announcement,
    isPickedUp,
    displayColumn,
    handleKeydown,
    cancelMove,
    commitMove,
    COLUMN_LABELS,
  }
}
