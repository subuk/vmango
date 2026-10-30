(() => {
  const media = matchMedia('(prefers-color-scheme: dark)');
  const read = () => { try { return localStorage.getItem('td-color-theme'); } catch { return null; } };
  let mode = read();
  if (!['light', 'dark', 'auto'].includes(mode)) mode = 'auto';
  const apply = () => {
    const theme = mode === 'auto' ? (media.matches ? 'dark' : 'light') : mode;
    document.documentElement.dataset.bsTheme = theme;
    document.querySelector('meta[name="theme-color"]').content = theme === 'dark' ? '#181b20' : '#ffffff';
  };
  apply();
  media.addEventListener('change', apply);
  addEventListener('DOMContentLoaded', () => {
    const button = document.querySelector('#theme-mode');
    const modes = ['light', 'dark', 'auto'];
    const labels = { light: 'Light', dark: 'Dark', auto: 'System' };
    const paths = {
      light: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/>',
      dark: '<path d="M20.5 13A8.5 8.5 0 0 1 11 3.5 8.5 8.5 0 1 0 20.5 13Z"/>',
      auto: '<rect x="3" y="4" width="18" height="13" rx="2"/><path d="M12 17v4m-4 0h8"/>'
    };
    const update = () => {
      const next = modes[(modes.indexOf(mode) + 1) % modes.length];
      button.innerHTML = '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' + paths[mode] + '</svg>';
      button.title = 'Theme: ' + labels[mode];
      button.setAttribute('aria-label', 'Color theme: ' + labels[mode] + '. Switch to ' + labels[next]);
    };
    update();
    button.addEventListener('click', () => {
      mode = modes[(modes.indexOf(mode) + 1) % modes.length];
      try { localStorage.setItem('td-color-theme', mode); } catch {}
      apply();
      update();
    });
  });
})();
