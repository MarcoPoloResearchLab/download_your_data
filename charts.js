export function countChart({
  title,
  counts,
  emptyLabel,
  tableLabel,
  valueLabel,
  formatLabel = (value) => value
}) {
  const panel = element('section', {class: 'panel chart-panel'});
  panel.append(element('h3', {text: title}));
  if (!counts.length) {
    panel.append(element('p', {class: 'empty-copy', text: emptyLabel}));
    return panel;
  }

  const maximum = Math.max(...counts.map((count) => count.value), 1);
  const leading = counts[0];
  panel.append(
    element('p', {
      class: 'chart-summary',
      text: `${formatLabel(leading.label)}: ${number(leading.value)} ${valueLabel.toLowerCase()}`
    })
  );

  const bars = element('div', {
    class: 'bar-chart',
    role: 'img',
    'aria-label': title
  });
  for (const count of counts) {
    const row = element('div', {class: 'bar-row'});
    row.append(
      element('span', {class: 'bar-label', text: formatLabel(count.label)}),
      element('progress', {
        class: 'bar-meter',
        max: String(maximum),
        value: String(count.value),
        'aria-label': `${formatLabel(count.label)}: ${number(count.value)}`
      }),
      element('span', {class: 'bar-value', text: number(count.value)})
    );
    bars.append(row);
  }
  panel.append(bars, countTable(counts, tableLabel, valueLabel, formatLabel));
  return panel;
}

export function seriesChart({
  title,
  labels,
  series,
  emptyLabel,
  tableLabel,
  valueLabel,
  formatLabel = (value) => value,
  formatSeries = (value) => value
}) {
  const panel = element('section', {class: 'panel chart-panel chart-panel-wide'});
  panel.append(element('h3', {text: title}));
  if (!labels.length || !series.length) {
    panel.append(element('p', {class: 'empty-copy', text: emptyLabel}));
    return panel;
  }

  const totals = labels.map((_, labelIndex) =>
    series.reduce((sum, currentSeries) => sum + (currentSeries.values[labelIndex] || 0), 0)
  );
  const peakIndex = totals.indexOf(Math.max(...totals));
  panel.append(
    element('p', {
      class: 'chart-summary',
      text: `${formatLabel(labels[peakIndex])}: ${number(totals[peakIndex])} ${valueLabel.toLowerCase()}`
    })
  );

  const chart = element('div', {
    class: 'series-chart',
    role: 'img',
    'aria-label': title
  });
  labels.forEach((label, labelIndex) => {
    const group = element('div', {class: 'series-group'});
    group.append(element('strong', {class: 'series-axis-label', text: formatLabel(label)}));
    const groupMaximum = Math.max(
      ...series.map((currentSeries) => currentSeries.values[labelIndex] || 0),
      1
    );
    series.forEach((currentSeries, seriesIndex) => {
      const value = currentSeries.values[labelIndex] || 0;
      const row = element('div', {class: 'series-row'});
      row.append(
        element('span', {
          class: `legend-swatch series-${seriesIndex % 8}`,
          'aria-hidden': 'true'
        }),
        element('span', {class: 'series-name', text: formatSeries(currentSeries.label)}),
        element('progress', {
          class: `series-meter series-${seriesIndex % 8}`,
          max: String(groupMaximum),
          value: String(value),
          'aria-label': `${formatSeries(currentSeries.label)}: ${number(value)}`
        }),
        element('span', {class: 'bar-value', text: number(value)})
      );
      group.append(row);
    });
    chart.append(group);
  });
  panel.append(chart, seriesLegend(series, formatSeries, title));

  const details = element('details', {class: 'chart-data'});
  details.append(element('summary', {text: tableLabel}));
  const table = element('table');
  const headRow = element('tr');
  headRow.append(element('th', {scope: 'col', text: tableLabel}));
  series.forEach((currentSeries) => {
    headRow.append(
      element('th', {scope: 'col', text: formatSeries(currentSeries.label)})
    );
  });
  const head = element('thead');
  head.append(headRow);
  const body = element('tbody');
  labels.forEach((label, labelIndex) => {
    const row = element('tr');
    row.append(element('th', {scope: 'row', text: formatLabel(label)}));
    series.forEach((currentSeries) => {
      row.append(
        element('td', {text: number(currentSeries.values[labelIndex] || 0)})
      );
    });
    body.append(row);
  });
  table.append(head, body);
  details.append(element('div', {class: 'table-scroll'}, table));
  panel.append(details);
  return panel;
}

function countTable(counts, tableLabel, valueLabel, formatLabel) {
  const details = element('details', {class: 'chart-data'});
  details.append(element('summary', {text: tableLabel}));
  const table = element('table');
  const head = element('thead');
  const headRow = element('tr');
  headRow.append(
    element('th', {scope: 'col', text: tableLabel}),
    element('th', {scope: 'col', text: valueLabel})
  );
  head.append(headRow);
  const body = element('tbody');
  for (const count of counts) {
    const row = element('tr');
    row.append(
      element('th', {scope: 'row', text: formatLabel(count.label)}),
      element('td', {text: number(count.value)})
    );
    body.append(row);
  }
  table.append(head, body);
  details.append(element('div', {class: 'table-scroll'}, table));
  return details;
}

function seriesLegend(series, formatSeries, title) {
  const legend = element('ul', {class: 'chart-legend', 'aria-label': title});
  series.forEach((currentSeries, seriesIndex) => {
    const item = element('li');
    item.append(
      element('span', {
        class: `legend-swatch series-${seriesIndex % 8}`,
        'aria-hidden': 'true'
      }),
      document.createTextNode(formatSeries(currentSeries.label))
    );
    legend.append(item);
  });
  return legend;
}

function element(tagName, attributes = {}, ...children) {
  const node = document.createElement(tagName);
  for (const [name, value] of Object.entries(attributes)) {
    if (name === 'class') {
      node.className = value;
    } else if (name === 'text') {
      node.textContent = value;
    } else {
      node.setAttribute(name, value);
    }
  }
  node.append(...children.flat().filter((child) => child !== undefined && child !== null));
  return node;
}

function number(value) {
  return new Intl.NumberFormat(document.documentElement.lang).format(value);
}
