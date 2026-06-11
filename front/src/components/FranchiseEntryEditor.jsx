const statusOptions = [
  { value: 'watching', label: 'Watching' },
  { value: 'completed', label: 'Completed' },
  { value: 'on_hold', label: 'On hold' },
  { value: 'dropped', label: 'Dropped' },
  { value: 'plan_to_watch', label: 'Plan to watch' },
]

const REMOVE_FROM_LIST_VALUE = 'remove_from_list'

function FranchiseEntryEditor({ item, isPending, onUpdateEntry, onRemoveEntry }) {
  const handleStatusChange = (event) => {
    const status = event.target.value
    if (status === REMOVE_FROM_LIST_VALUE) {
      void onRemoveEntry(item.id)
      return
    }
    if (!status || status === item.user_list_status) {
      return
    }
    void onUpdateEntry(item.id, { status })
  }

  if (!item.in_user_list) {
    return (
      <div className="franchise-edit">
        <label className="franchise-edit-field">
          <span className="field-label">Add to list</span>
          <select
            className="select-input franchise-edit-select"
            value=""
            disabled={isPending}
            onChange={handleStatusChange}
          >
            <option value="" disabled>
              Choose status...
            </option>
            {statusOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
      </div>
    )
  }

  return (
    <div className="franchise-edit">
      <label className="franchise-edit-field">
        <span className="field-label">Status</span>
        <select
          className="select-input franchise-edit-select"
          value={item.user_list_status ?? ''}
          disabled={isPending}
          onChange={handleStatusChange}
        >
          {statusOptions.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
          <option value={REMOVE_FROM_LIST_VALUE}>Remove from list</option>
        </select>
      </label>
    </div>
  )
}

export default FranchiseEntryEditor
