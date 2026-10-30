(function () {
  var preference = 'system';
  try { var value = localStorage.getItem('vmango.theme'); if (value === 'light' || value === 'dark') preference = value; } catch (_) {}
  var dark = preference === 'dark' || preference === 'system' && matchMedia('(prefers-color-scheme: dark)').matches;
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
  document.documentElement.style.backgroundColor = dark ? '#171b20' : '#fafbfc';
})();
