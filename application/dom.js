// @ts-check

export function element(tagName, attributes = {}, ...children) {
  const node = document.createElement(tagName);
  for (const [name, value] of Object.entries(attributes)) {
    if (name === 'class') {
      node.className = value;
    } else if (name === 'text') {
      node.textContent = value;
    } else if (['disabled', 'selected', 'checked'].includes(name)) {
      node[name] = Boolean(value);
    } else if (value !== undefined && value !== null) {
      node.setAttribute(name, String(value));
    }
  }
  node.append(...children.flat().filter((child) => child !== undefined && child !== null));
  return node;
}
