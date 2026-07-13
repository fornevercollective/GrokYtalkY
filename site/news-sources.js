/**
 * Major live news sources — mirrors news_wall.go MajorNewsSources().
 * Used by livenews.html (and stress tools).
 */
(function (global) {
  'use strict';
  const MAJOR_NEWS = [
    { id: 'aje', label: 'Al Jazeera', url: 'https://www.youtube.com/@AlJazeeraEnglish/live', region: 'me', hue: 200 },
    { id: 'f24', label: 'France 24', url: 'https://www.youtube.com/@France24_en/live', region: 'eu', hue: 210 },
    { id: 'dw', label: 'DW News', url: 'https://www.youtube.com/@dwnews/live', region: 'eu', hue: 220 },
    { id: 'sky', label: 'Sky News', url: 'https://www.youtube.com/@SkyNews/live', region: 'eu', hue: 195 },
    { id: 'abc', label: 'ABC News', url: 'https://www.youtube.com/@ABCNews/live', region: 'us', hue: 0 },
    { id: 'nbc', label: 'NBC News', url: 'https://www.youtube.com/@NBCNews/live', region: 'us', hue: 35 },
    { id: 'eur', label: 'Euronews', url: 'https://www.youtube.com/@euronews/live', region: 'eu', hue: 185 },
    { id: 'bbg', label: 'Bloomberg', url: 'https://www.youtube.com/@BloombergTelevision/live', region: 'us', hue: 45 },
    { id: 'cspan', label: 'C-SPAN', url: 'https://www.youtube.com/@cspan/live', region: 'us', hue: 260 },
    { id: 'pbs', label: 'PBS News', url: 'https://www.youtube.com/@PBSNewsHour/live', region: 'us', hue: 170 },
    { id: 'reu', label: 'Reuters', url: 'https://www.youtube.com/@Reuters/live', region: 'world', hue: 15 },
    { id: 'nhk', label: 'NHK World', url: 'https://www.youtube.com/@NHKWORLDJAPAN/live', region: 'asia', hue: 330 },
  ];
  const REGIONS = [
    { id: 'us', label: 'United States' },
    { id: 'eu', label: 'Europe' },
    { id: 'me', label: 'Middle East' },
    { id: 'asia', label: 'Asia' },
    { id: 'world', label: 'World' },
  ];
  global.GY_NEWS = { MAJOR_NEWS, REGIONS };
})(typeof window !== 'undefined' ? window : globalThis);
