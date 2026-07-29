// Global variable to hold version, default is the static fallback from HTML
window.alpsVersion = 'v1.0';

// Copy to clipboard function
function copyInstall(btn) {
  const text = 'curl -fsSL https://alps-project.pages.dev/install.sh | sh';
  navigator.clipboard.writeText(text).then(() => {
	btn.textContent = 'copied';
	btn.classList.add('copied');
	setTimeout(() => {
	  btn.textContent = 'copy';
	  btn.classList.remove('copied');
	}, 2000);
  });
}

// Dynamic layout state for setting active architecture and link
function setArch(btn, type, arch) {
  const list = btn.parentElement;
  for (const child of list.children) {
	child.classList.remove('active');
  }
  btn.classList.add('active');
  
  const link = document.getElementById(type === 'linux' ? 'ver-dl-link' : 'dl-' + type + '-link');
  const text = document.getElementById('dl-' + type + '-text');
  if (!link || !text) return;
  
  const v = window.alpsVersion || 'v1.0';
  const pureV = v.replace(/^v/, '');
  
  if (type === 'deb') {
	link.href = `https://github.com/adrianpriza-ai/alps/releases/download/${v}/alps_${pureV}_${arch}.deb`;
  } else if (type === 'termux') {
	link.href = `https://github.com/adrianpriza-ai/alps/releases/download/${v}/alps-termux-${arch}`;
  } else if (type === 'linux') {
	link.href = `https://github.com/adrianpriza-ai/alps/releases/download/${v}/alps-linux-${arch}`;
  } else if (type === 'macos') {
	link.href = `https://github.com/adrianpriza-ai/alps/releases/download/${v}/alps-darwin-${arch}`;
  }
  text.textContent = `Download for ${arch}`;
}

// Helper to find and trigger setArch on the correct button
function triggerArch(type, arch) {
  const list = document.getElementById('arch-list-' + type);
  if (list) {
	const btn = Array.from(list.children).find(b => b.textContent.trim() === arch);
	if (btn) {
	  setArch(btn, type, arch);
	}
  }
}

// Re-apply links when version is dynamically fetched
function refreshDownloadLinks() {
  ['deb', 'termux', 'linux', 'macos'].forEach(type => {
    const list = document.getElementById('arch-list-' + type);
    if (list) {
      const activeBtn = list.querySelector('.dl-widget-tab.active');
      if (activeBtn) {
        const arch = activeBtn.textContent.trim();
        setArch(activeBtn, type, arch);
      }
    }
  });
}

// Sync initialization of arch detection & default links
function initDownloads() {
  // 1. Get fallback version from DOM if possible
  const heroNum = document.getElementById('ver-hero-num');
  if (heroNum && heroNum.textContent.trim()) {
    window.alpsVersion = heroNum.textContent.trim();
  }

  // 2. Perform client-side CPU architecture detection
  let archLinux = 'amd64';
  let archDeb = 'amd64';
  let archTermux = 'aarch64';
  let archMacos = 'amd64';

  const ua = navigator.userAgent.toLowerCase();
  const platform = (navigator.platform || '').toLowerCase();

  const isArm64 = ua.includes('aarch64') || ua.includes('arm64') ||
                  platform.includes('arm64') || platform.includes('aarch64');
  const isArm32 = (ua.includes('arm') || platform.includes('arm')) && !ua.includes('aarch64') && !ua.includes('arm64') && !platform.includes('arm64');

  if (isArm64) {
    archLinux = 'arm64';
    archDeb = 'arm64';
    archTermux = 'aarch64';
    archMacos = 'arm64';
  } else if (isArm32) {
    archLinux = 'armv7';
    archDeb = 'armhf';
    archTermux = 'arm';
    // macOS doesn't have 32-bit variant, keep as x86_64
  } else {
    // x86_64
    if (ua.includes('x86_64') || platform.includes('x86_64') || platform.includes('win64') || platform.includes('wow64') || ua.includes('amd64')) {
      archLinux = 'amd64';
      archDeb = 'amd64';
      archTermux = 'x86_64';
      archMacos = 'amd64';
    }
  }

  // 3. Trigger initial setup for each category
  triggerArch('linux', archLinux);
  triggerArch('deb', archDeb);
  triggerArch('termux', archTermux);
  triggerArch('macos', archMacos);

// 4. Try high entropy userAgentData for modern browsers (async fallback)
if (navigator.userAgentData && navigator.userAgentData.getHighEntropyValues) {
  navigator.userAgentData.getHighEntropyValues(['architecture']).then(values => {
    const entropyArch = (values.architecture || '').toLowerCase();
    if (entropyArch === 'arm64' || entropyArch === 'aarch64') {
      triggerArch('linux', 'arm64');
      triggerArch('deb', 'arm64');
      triggerArch('termux', 'aarch64');
      triggerArch('macos', 'arm64');
    } else if (entropyArch === 'arm') {
      triggerArch('linux', 'armv7');
      triggerArch('deb', 'armhf');
      triggerArch('termux', 'arm');
      // macOS doesn't have 32-bit ARM variant
    } else if (entropyArch === 'x86_64') {
      triggerArch('linux', 'amd64');
      triggerArch('deb', 'amd64');
      triggerArch('termux', 'x86_64');
      triggerArch('macos', 'amd64');
    }
  }).catch(() => {});
}
}

// Async fetch version from GitHub API
async function fetchVersion() {
  try {
	const res = await fetch(
	  'https://api.github.com/repos/adrianpriza-ai/alps/releases/latest',
	  { headers: { Accept: 'application/vnd.github+json' } }
	);
	if (!res.ok) return;
	const { tag_name } = await res.json();
	if (!tag_name) return;
	const v = tag_name.startsWith('v') ? tag_name : 'v' + tag_name;

	window.alpsVersion = v;

	/* Update version labels on the page */
	const navTag = document.getElementById('ver-nav');
	if (navTag) navTag.textContent = v;

	const heroNum = document.getElementById('ver-hero-num');
	if (heroNum) heroNum.textContent = v;

	const footerMeta = document.getElementById('ver-footer');
	if (footerMeta) footerMeta.textContent = v + ' \u00a0/\u00a0 MIT License \u00a0/\u00a0 Go 1.22+';

	/* Update active download links with the newly fetched version tag */
	refreshDownloadLinks();
  } catch (_) { /* Keep static fallback if offline */ }
}

// Kickstart the processes
initDownloads();
fetchVersion();
