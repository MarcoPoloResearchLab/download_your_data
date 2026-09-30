// @ts-check

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
