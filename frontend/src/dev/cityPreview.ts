// SPDX-License-Identifier: AGPL-3.0-only
//
// Mounts City alone against the mockup estate (#978/#979 review
// screenshots). Reached only through dev/city-preview.html on the dev
// server; that page is not an input to `vite build` (index.html is),
// so nothing here ships. `?stop=` picks one of lib/city/project.ts's
// STOPS (default "city"); anything else falls back to "city" rather
// than mounting with a bad prop.

import { mount } from 'svelte'
import '../app.css'
import City from '../components/City.svelte'
import { mockupEstate } from '../lib/city/fixture'
import { layoutGround } from '../lib/city/layout'
import { STOPS, type Stop } from '../lib/city/project'

const params = new URLSearchParams(location.search)
const requested = params.get('stop')
const stop: Stop = (STOPS as readonly string[]).includes(requested ?? '') ? (requested as Stop) : 'city'

const ground = layoutGround(mockupEstate())

export default mount(City, {
  target: document.getElementById('stage')!,
  props: { stop, ground },
})
