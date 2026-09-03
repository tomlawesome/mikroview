// SPDX-License-Identifier: AGPL-3.0-only
//
// Mounts the city's device-library contact sheet (#864). Reached only
// through dev/city-devices.html on the dev server; that page is not an
// input to `vite build` (index.html is), so nothing here ships.

import { mount } from 'svelte'
import '../app.css'
import CityDeviceGallery from '../components/CityDeviceGallery.svelte'

export default mount(CityDeviceGallery, {
  target: document.getElementById('gallery')!,
})
