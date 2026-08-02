export type ThemePref = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'mikroview-theme'

function loadInitial(): ThemePref {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    return v === 'light' || v === 'dark' ? v : 'system'
  } catch {
    return 'system'
  }
}

// Reflects the chosen theme preference onto <html data-theme="...">, which
// app.css keys its light/dark variable overrides off of. 'system' removes
// the attribute entirely, falling back to the prefers-color-scheme media
// query in app.css.
class ThemeState {
  pref = $state<ThemePref>(loadInitial())

  apply() {
    const root = document.documentElement
    if (this.pref === 'system') {
      root.removeAttribute('data-theme')
    } else {
      root.setAttribute('data-theme', this.pref)
    }
    try {
      localStorage.setItem(STORAGE_KEY, this.pref)
    } catch {
      // storage unavailable (private browsing, etc.) -- theme just won't persist
    }
  }

  cycle() {
    const order: ThemePref[] = ['system', 'light', 'dark']
    this.pref = order[(order.indexOf(this.pref) + 1) % order.length]
  }
}

export const themeState = new ThemeState()
