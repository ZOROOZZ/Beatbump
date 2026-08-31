import { build, files, version } from "$service-worker";

// Create a unique cache name for this deployment
const CACHE = `cache-${version}`;

const ASSETS = [
	...build, // the app itself
	...files, // everything in `static`
];

self.addEventListener("install", (event) => {
	// Create a new cache and add all files to it
	async function addFilesToCache() {
		const cache = await caches.open(CACHE);
		await cache.addAll(ASSETS);
	}

	event.waitUntil(addFilesToCache());
});

self.addEventListener("activate", (event) => {
	// Remove previous cached data from disk
	async function deleteOldCaches() {
		for (const key of await caches.keys()) {
			if (key !== CACHE) await caches.delete(key);
		}
	}

	event.waitUntil(deleteOldCaches());
});

self.addEventListener("fetch", (event) => {
	// ignore POST requests etc
	if (event.request.method !== "GET") return;

	const url = new URL(event.request.url);

	// Never touch cross-origin requests (YouTube CDN streams, the audio/video
	// proxy, the API, etc). Audio/video playback and range-request seeking
	// must go straight to the network - caching or replaying them from here
	// can corrupt streaming playback and has nothing to do with "offline app
	// shell" caching, which is all this service worker is meant to handle.
	if (url.origin !== self.location.origin) return;

	// Same-origin API calls should also always hit the network directly.
	if (url.pathname.startsWith("/api/")) return;

	async function respond() {
		const cache = await caches.open(CACHE);

		// `build`/`files` can always be served from the cache
		if (ASSETS.includes(url.pathname)) {
			return (await cache.match(event.request)) ?? fetch(event.request);
		}

		// for everything else, try the network first, but
		// fall back to the cache if we're offline
		try {
			const response = await fetch(event.request);

			if (response.status === 200) {
				await cache.put(event.request, response.clone());
			}

			return response;
		} catch (error) {
			const cached = await cache.match(event.request);
			if (cached) return cached;
			throw error;
		}
	}

	event.respondWith(respond());
});
