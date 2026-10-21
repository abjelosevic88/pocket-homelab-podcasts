import { Modal } from './ui'
import { isMac } from './CommandPalette'

const mod = isMac ? '⌘' : 'Ctrl'
const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['h'], 'Home'], [['p'], 'Podcasts'], [['e'], 'Episodes'], [['u'], 'Up next'], [['d'], 'Downloads'], [['a'], 'Add a podcast'], [[','], 'Settings']] },
  { title: 'Search', keys: [[['/'], 'Search everything'], [[mod, 'K'], 'Search everything'], [['tab'], 'Next scope in the search']] },
  { title: 'Player', keys: [[['space'], 'Play or pause'], [['←', '→'], 'Skip back or forward'], [['shift', '→'], 'A minute forward'], [['<', '>'], 'Slower or faster'], [['shift', 'N'], 'Next in the queue']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close a dialog, menu or the player'], [['?'], 'This sheet']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
      <p className="muted small mt">The player keys work whenever an episode is loaded, on every page. j and l skip too, k plays or pauses.</p>
    </Modal>
  )
}
