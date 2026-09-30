// @ts-check

import {element} from './dom.js';
import {countChart} from './charts.js';

const drawings = new WeakMap();
const SVG_NAMESPACE = 'http://www.w3.org/2000/svg';
const PALETTE = [
  'var(--accent)',
  'var(--info)',
  'var(--warning)',
  'var(--success)',
  'var(--danger)',
  'var(--chart-purple)',
  'var(--chart-pink)',
  'var(--muted)'
];
function color(label) {
  if (label === 'movie') return PALETTE[0];
  if (label === 'series') return PALETTE[1];
  if (label === 'unknown') return PALETTE[7];
  let hash = 0;
  for (const character of label)
    hash = (hash * 31 + character.charCodeAt(0)) >>> 0;
  return PALETTE[hash % 7];
}
function svgElement(name, attributes = {}, text = '') {
  const node = document.createElementNS(SVG_NAMESPACE, name);
  for (const [key, value] of Object.entries(attributes))
    node.setAttribute(key, String(value));
  if (text) node.textContent = text;
  return node;
}
function details(labels, series, copy, formatLabel, formatSeries) {
  const head = element(
    'tr',
    {},
    element('th', {scope: 'col', text: copy.data_table}),
    ...series.map((item) =>
      element('th', {scope: 'col', text: formatSeries(item.label)})
    )
  );
  return element(
    'details',
    {class: 'chart-data'},
    element('summary', {text: copy.data_table}),
    element(
      'div',
      {class: 'table-scroll'},
      element(
        'table',
        {},
        element('thead', {}, head),
        element(
          'tbody',
          {},
          ...labels.map((label, index) =>
            element(
              'tr',
              {},
              element('th', {scope: 'row', text: formatLabel(label)}),
              ...series.map((item) =>
                element('td', {text: String(item.values[index])})
              )
            )
          )
        )
      )
    )
  );
}
function identify(panel, name) {
  panel.setAttribute('data-media-chart', name);
  return panel;
}
function countPanel(name, title, values, copy, formatLabel = (value) => value) {
  const panel = identify(
    countChart({
      title,
      counts: values.map((item) => ({label: item.label, value: item.count})),
      emptyLabel: copy.chartEmpty,
      tableLabel: copy.data_table,
      valueLabel: copy.activities,
      formatLabel
    }),
    name
  );
  if (name === 'genres')
    panel.querySelectorAll('.bar-meter').forEach((meter, index) => {
      /** @type {HTMLElement} */ (meter).style.setProperty(
        '--bar-color',
        color(values[index].label)
      );
    });
  return panel;
}
function mediaSplit(overview, copy) {
  const values = overview.media_types;
  const panel = identify(
    element(
      'section',
      {class: 'panel chart-panel'},
      element('h2', {text: copy.mediaSplit})
    ),
    'media-types'
  );
  if (!values.length) {
    panel.append(element('p', {class: 'empty-copy', text: copy.chartEmpty}));
    return panel;
  }
  const total = values.reduce((sum, item) => sum + item.count, 0);
  let offset = 0;
  const gradient = values
    .map((item) => {
      const start = offset;
      offset += (item.count / total) * 100;
      return `${color(item.label)} ${start}% ${offset}%`;
    })
    .join(',');
  const diagram = element(
    'div',
    {
      class: 'media-donut',
      role: 'img',
      'aria-label': values
        .map((item) => `${typeLabel(item.label, copy)}: ${item.count}`)
        .join('; ')
    },
    element(
      'div',
      {class: 'media-donut-hole'},
      element('strong', {text: String(total)}),
      element('small', {text: copy.activities})
    )
  );
  diagram.style.background = `conic-gradient(${gradient})`;
  const legend = element('div', {class: 'media-split-values'});
  for (const item of values) {
    const swatch = element('span', {
      class: 'legend-swatch',
      'aria-hidden': 'true'
    });
    swatch.style.background = color(item.label);
    legend.append(
      element(
        'div',
        {},
        swatch,
        element('span', {text: typeLabel(item.label, copy)}),
        element('strong', {
          text: `${Math.round((item.count / total) * 100)}%`
        }),
        element('small', {
          text: `${item.count} ${copy.activities.toLowerCase()}`
        })
      )
    );
  }
  panel.append(
    element('div', {class: 'media-donut-layout'}, diagram, legend),
    details(
      values.map((item) => item.label),
      [{label: copy.activities, values: values.map((item) => item.count)}],
      copy,
      (label) => typeLabel(label, copy),
      (value) => value
    )
  );
  return panel;
}
function typeLabel(label, copy) {
  return {movie: copy.films, series: copy.series, unknown: copy.unknown}[label];
}
function plot({
  name,
  title,
  labels,
  series,
  kind,
  copy,
  formatLabel,
  formatSeries,
  caption = ''
}) {
  const panel = identify(
    element(
      'section',
      {
        class: `panel chart-panel ${name === 'monthly' || name === 'year-genres' ? 'chart-panel-wide' : ''}`
      },
      element(
        'div',
        {class: 'media-chart-heading'},
        element('h2', {text: title}),
        caption ? element('small', {text: caption}) : null
      )
    ),
    name
  );
  if (!labels.length || !series.length) {
    panel.append(element('p', {class: 'empty-copy', text: copy.chartEmpty}));
    return panel;
  }
  const hidden = new Set();
  const legend = element('div', {
    class: 'media-chart-legend',
    'aria-label': title
  });
  const host = element('div', {class: 'media-plot'});
  const tooltip = element('div', {
    class: 'media-chart-tooltip',
    role: 'status',
    hidden: true
  });
  for (const item of series) {
    const swatch = element('span', {
      class: 'legend-swatch',
      'aria-hidden': 'true'
    });
    swatch.style.background = color(item.label);
    const button = element(
      'button',
      {
        type: 'button',
        'aria-pressed': 'true',
        'data-chart-series': item.label
      },
      swatch,
      formatSeries(item.label)
    );
    button.addEventListener('click', () => {
      if (hidden.has(item.label)) hidden.delete(item.label);
      else hidden.add(item.label);
      button.setAttribute('aria-pressed', String(!hidden.has(item.label)));
      draw();
    });
    legend.append(button);
  }
  panel.append(
    legend,
    host,
    tooltip,
    details(labels, series, copy, formatLabel, formatSeries)
  );
  function draw() {
    const width = host.clientWidth;
    if (!width) return;
    host.replaceChildren();
    tooltip.hidden = true;
    const height = kind === 'line' ? 240 : 225,
      left = 64,
      right = 20,
      top = 18,
      bottom = 55,
      plotWidth = width - left - right,
      plotHeight = height - top - bottom;
    const active = series.filter((item) => !hidden.has(item.label));
    const maximum = Math.max(
      ...labels.map((_, i) =>
        kind === 'line'
          ? Math.max(...active.map((item) => item.values[i]), 0)
          : active.reduce((sum, item) => sum + item.values[i], 0)
      ),
      1
    );
    const step = Math.max(1, Math.ceil(maximum / 4));
    const ceiling = step * 4;
    const y = (value) => height - bottom - (value / ceiling) * plotHeight;
    const x = (index) =>
      left +
      (labels.length === 1
        ? plotWidth / 2
        : (index / (labels.length - 1)) * plotWidth);
    const bandWidth = plotWidth / labels.length;
    const svg = svgElement('svg', {
      class: 'media-chart-svg',
      viewBox: `0 0 ${width} ${height}`,
      role: 'img',
      'aria-label': `${title}. ${series.map((item) => `${formatSeries(item.label)}: ${item.values.join(', ')}`).join('; ')}`
    });
    svg.append(
      svgElement('title', {}, title),
      svgElement(
        'desc',
        {},
        labels
          .map(
            (label, i) =>
              `${formatLabel(label)}: ${series.map((item) => `${formatSeries(item.label)} ${item.values[i]}`).join(', ')}`
          )
          .join('; ')
      )
    );
    for (let tick = 0; tick <= 4; tick++) {
      const value = tick * step;
      svg.append(
        svgElement('line', {
          x1: left,
          x2: width - right,
          y1: y(value),
          y2: y(value),
          stroke: 'var(--border)'
        }),
        svgElement(
          'text',
          {x: left - 6, y: y(value) + 4, 'text-anchor': 'end'},
          String(value)
        )
      );
    }
    svg.append(
      svgElement(
        'text',
        {
          class: 'axis-title',
          'data-axis': 'y',
          transform: `translate(15,${top + plotHeight / 2}) rotate(-90)`,
          'text-anchor': 'middle'
        },
        copy.activities
      ),
      svgElement(
        'text',
        {
          class: 'axis-title',
          'data-axis': 'x',
          x: left + plotWidth / 2,
          y: height - 8,
          'text-anchor': 'middle'
        },
        name === 'weekday-genres'
          ? copy.weekday
          : name === 'year-genres'
            ? copy.viewingYear
            : copy.month
      )
    );
    svg.append(
      svgElement('rect', {
        'data-chart-frame': '',
        x: left,
        y: top,
        width: plotWidth,
        height: plotHeight,
        fill: 'none',
        stroke: 'var(--border)'
      })
    );
    const every = Math.max(
      1,
      Math.ceil(labels.length / (width < 350 ? 4 : Math.floor(plotWidth / 45)))
    );
    for (let i = 0; i < labels.length; i++)
      if (i % every === 0 || i === labels.length - 1)
        svg.append(
          svgElement(
            'text',
            {
              x: kind === 'bar' ? left + (i + 0.5) * bandWidth : x(i),
              y: height - bottom + 20,
              'text-anchor':
                kind === 'bar'
                  ? 'middle'
                  : i === 0
                    ? 'start'
                    : i === labels.length - 1
                      ? 'end'
                      : 'middle'
            },
            formatLabel(labels[i])
          )
        );
    const offsets = labels.map(() => 0);
    for (const item of active) {
      if (kind === 'bar')
        item.values.forEach((value, index) => {
          svg.append(
            svgElement('rect', {
              x: left + (index + 0.14) * bandWidth,
              y: y(offsets[index] + value),
              width: bandWidth * 0.72,
              height: y(offsets[index]) - y(offsets[index] + value),
              fill: color(item.label)
            })
          );
          offsets[index] += value;
        });
      else if (kind === 'area') {
        const upper = item.values.map(
          (value, index) => `${x(index)},${y(offsets[index] + value)}`
        );
        const lower = item.values
          .map((_, index) => `${x(index)},${y(offsets[index])}`)
          .reverse();
        svg.append(
          svgElement('path', {
            d: `M${upper.join(' L')} L${lower.join(' L')} Z`,
            fill: color(item.label),
            'fill-opacity': '.72'
          })
        );
        item.values.forEach((value, index) => {
          offsets[index] += value;
        });
      } else {
        svg.append(
          svgElement('path', {
            d: item.values
              .map(
                (value, index) =>
                  `${index === 0 ? 'M' : 'L'}${x(index)},${y(value)}`
              )
              .join(' '),
            fill: 'none',
            stroke: color(item.label),
            'stroke-width': 2
          })
        );
        item.values.forEach((value, index) =>
          svg.append(
            svgElement('circle', {
              cx: x(index),
              cy: y(value),
              r: 3,
              fill: color(item.label)
            })
          )
        );
      }
    }
    const guide = svgElement('line', {
      'data-chart-hover-guide': '',
      y1: top,
      y2: height - bottom,
      stroke: 'var(--muted)',
      visibility: 'hidden'
    });
    svg.append(guide);
    const hit = svgElement('rect', {
      'data-chart-hit': '',
      x: left,
      y: top,
      width: plotWidth,
      height: plotHeight,
      fill: 'transparent'
    });
    const inspect = (event) => {
      const bounds = svg.getBoundingClientRect();
      const position = Math.max(
        0,
        Math.min(plotWidth, event.clientX - bounds.left - left)
      );
      const index = Math.min(
        labels.length - 1,
        Math.round((position / plotWidth) * (labels.length - 1))
      );
      guide.setAttribute('x1', String(left + position));
      guide.setAttribute('x2', String(left + position));
      guide.setAttribute('visibility', 'visible');
      tooltip.replaceChildren(
        element('strong', {text: formatLabel(labels[index])}),
        ...active.map((item) =>
          element(
            'div',
            {},
            element('span', {text: formatSeries(item.label)}),
            element('span', {text: String(item.values[index])})
          )
        )
      );
      tooltip.hidden = false;
    };
    hit.addEventListener('pointermove', inspect);
    hit.addEventListener('click', inspect);
    hit.addEventListener('pointerleave', () => {
      tooltip.hidden = true;
      guide.setAttribute('visibility', 'hidden');
    });
    svg.append(hit);
    host.append(svg);
  }
  drawings.set(host, draw);
  return panel;
}
function monthlyPeriods(values) {
  const observed = [...new Set(values.map((item) => item.period))].sort();
  if (!observed.length) return [];
  const index = (period) =>
    Number(period.slice(0, 4)) * 12 + Number(period.slice(5)) - 1;
  const last = index(observed[observed.length - 1]);
  const first = Math.max(index(observed[0]), last - 11);
  return Array.from({length: last - first + 1}, (_, offset) => {
    const month = first + offset;
    return `${String(Math.floor(month / 12)).padStart(4, '0')}-${String((month % 12) + 1).padStart(2, '0')}`;
  });
}

/** @param {import('./api.js').MediaReport['overview']} overview */
export function dashboardCharts(overview, copy, locale) {
  const grid = element('div', {class: 'media-chart-grid'});
  const monthLabels = monthlyPeriods(overview.monthly_media);
  const types = ['movie', 'series', 'unknown'].filter((label) =>
    overview.media_types.some((item) => item.label === label)
  );
  const seriesFor = (values, labels, keys) =>
    keys.map((label) => ({
      label,
      values: labels.map(
        (period) =>
          values.find((item) => item.period === period && item.label === label)
            ?.count ?? 0
      )
    }));
  const monthName = (month) =>
    new Intl.DateTimeFormat(locale, {month: 'short', timeZone: 'UTC'}).format(
      new Date(`${month}-01T00:00:00Z`)
    );
  grid.append(
    plot({
      name: 'monthly',
      title: copy.monthlyViewing,
      labels: monthLabels,
      series: seriesFor(overview.monthly_media, monthLabels, types),
      kind: 'line',
      copy,
      formatLabel: monthName,
      formatSeries: (label) => typeLabel(label, copy),
      caption: monthLabels.length
        ? `${monthLabels[0]} – ${monthLabels[monthLabels.length - 1]}`
        : ''
    }),
    mediaSplit(overview, copy)
  );
  const genres = countPanel('genres', copy.topGenres, overview.genres, copy);
  genres.append(element('small', {text: copy.genreOverlap}));
  grid.append(genres);
  const genreKeys = overview.genres.map((item) => item.label);
  const weekdayLabels = [
    'Monday',
    'Tuesday',
    'Wednesday',
    'Thursday',
    'Friday',
    'Saturday',
    'Sunday'
  ];
  const weekdayName = (day) =>
    new Intl.DateTimeFormat(locale, {
      weekday: 'short',
      timeZone: 'UTC'
    }).format(new Date(Date.UTC(2026, 1, 2 + weekdayLabels.indexOf(day))));
  grid.append(
    plot({
      name: 'weekday-genres',
      title: copy.weekdayGenres,
      labels: overview.genres_by_weekday.length ? weekdayLabels : [],
      series: seriesFor(overview.genres_by_weekday, weekdayLabels, genreKeys),
      kind: 'bar',
      copy,
      formatLabel: weekdayName,
      formatSeries: (label) => label
    })
  );
  const languages = countPanel(
    'original-languages',
    copy.originalLanguages,
    overview.original_languages,
    copy,
    (label) => new Intl.DisplayNames([locale], {type: 'language'}).of(label)
  );
  languages.append(element('small', {text: copy.originalLanguageNote}));
  grid.append(languages);
  const years = [
    ...new Set(overview.genres_by_year.map((item) => item.period))
  ].sort();
  grid.append(
    plot({
      name: 'year-genres',
      title: copy.yearGenres,
      labels: years,
      series: seriesFor(overview.genres_by_year, years, genreKeys),
      kind: years.length > 1 ? 'area' : 'bar',
      copy,
      formatLabel: (label) => label,
      formatSeries: (label) => label,
      caption: copy.filteredPeriods
    })
  );
  return grid;
}
export function observeDashboardCharts(root) {
  const observer = new ResizeObserver((entries) => {
    for (const entry of entries) drawings.get(entry.target)();
  });
  for (const host of root.querySelectorAll('.media-plot'))
    observer.observe(host);
  return () => observer.disconnect();
}
