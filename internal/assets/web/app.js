'use strict';

const $ = (selector) => document.querySelector(selector);
const elements = {
  rows: $('#order-items'),
  form: $('#order-form'),
  result: $('#result-content'),
  resultState: $('#result-state'),
  button: $('#submit-order'),
  buttonLabel: $('#submit-label'),
  day: $('#delivery-day'),
  time: $('#delivery-time'),
  count: $('#item-count'),
  history: $('#insight-bars')
};
let catalog = { products: [], warehouses: [] };
let isLoading = false;

// UI-only preference: never changes recommendation inputs or backend calculations.
function setTheme(theme, persist = false) {
  const dark = theme === 'dark';
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  const nextMode = dark ? 'light' : 'dark';
  const toggle = $('#theme-toggle');
  toggle.setAttribute('aria-label', `Switch to ${nextMode} mode`);
  toggle.setAttribute('title', `Switch to ${nextMode} mode`);
  toggle.setAttribute('aria-pressed', String(dark));
  toggle.querySelector('.theme-label').textContent = dark ? 'Light mode' : 'Dark mode';
  document.querySelector('meta[name="theme-color"]').content = dark ? '#111d1c' : '#f5f8f7';
  if (persist) {
    try { localStorage.setItem('warehouse-recommendation-theme', dark ? 'dark' : 'light'); }
    catch (_) { /* UI remains usable if storage is disabled. */ }
  }
}
setTheme(document.documentElement.dataset.theme);
$('#theme-toggle').addEventListener('click', () => {
  setTheme(document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark', true);
});

function node(tag, className, content) {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (content !== undefined) element.textContent = String(content);
  return element;
}
function productName(id) {
  const product = catalog.products.find((p) => p.id === id);
  return product ? product.name : id;
}
function productLabel(product) {
  return `${product.id} · ${product.name}`;
}
function setStatus(text, kind = 'neutral') {
  elements.resultState.textContent = text;
  elements.resultState.className = `result-state ${kind}`;
}
function showEmpty() {
  elements.result.replaceChildren();
  const wrap = node('div', 'empty-state');
  const icon = node('span', 'empty-icon', '◎'); icon.setAttribute('aria-hidden', 'true');
  wrap.append(icon, node('h3', '', 'Your best route starts here.'),
    node('p', '', 'Run the selection engine to get your warehouse recommendation and a breakdown of the decision.'));
  elements.result.append(wrap);
  setStatus('AWAITING ORDER');
}
function updateCount() {
  const n = elements.rows.children.length;
  elements.count.textContent = `${n} item${n === 1 ? '' : 's'}`;
  elements.rows.querySelectorAll('.remove-item').forEach((button) => {
    button.disabled = n === 1 || isLoading;
    button.setAttribute('aria-label', 'Remove product row');
  });
}
function addItem(id = 'P002', qty = 45) {
  const row = node('div', 'order-row');
  const wrap = node('label', 'input-wrap');
  const select = node('select', 'order-select');
  select.setAttribute('aria-label', 'Product');
  for (const product of catalog.products) {
    const option = node('option', '', productLabel(product));
    option.value = product.id;
    select.append(option);
  }
  select.value = catalog.products.some((p) => p.id === id) ? id : catalog.products[0]?.id;
  wrap.append(select);
  const input = node('input', 'qty-input');
  input.type = 'number'; input.min = '1'; input.step = '1'; input.required = true;
  input.value = qty;
  input.setAttribute('aria-label', 'Quantity');
  const remove = node('button', 'remove-item', '×');
  remove.type = 'button'; remove.title = 'Remove item';
  remove.addEventListener('click', () => { row.remove(); updateCount(); showEmpty(); });
  row.append(wrap, input, remove);
  elements.rows.append(row);
  updateCount();
}
function setPreset(name) {
  const examples = {
    routine: [{ product_id: 'P001', quantity: 250 }, { product_id: 'P003', quantity: 300 }],
    vaccine: [{ product_id: 'P002', quantity: 45 }],
    shortage: [{ product_id: 'P002', quantity: 109 }]
  };
  elements.rows.replaceChildren();
  for (const item of examples[name] || examples.vaccine) addItem(item.product_id, item.quantity);
  showEmpty();
}
function setLoading(value) {
  isLoading = value;
  elements.button.disabled = value;
  $('#add-item').disabled = value;
  elements.buttonLabel.textContent = value ? 'Evaluating combinations…' : 'Find optimal warehouses';
  for (const row of elements.rows.children) {
    row.querySelector('select').disabled = value;
    row.querySelector('input').disabled = value;
  }
  updateCount();
}
function makeMetric(label, amount, suffix = '') {
  const wrap = node('div', 'metric');
  wrap.append(node('small', '', label));
  const strong = node('strong', '', amount);
  if (suffix) strong.append(node('em', '', ` ${suffix}`));
  wrap.append(strong);
  return wrap;
}
function showHistoricalContext(info) {
  if (!info) return null;
  const card = node('div', 'historical-context');
  card.append(node('strong', '', 'HISTORICAL DELIVERY · OPTIONAL BONUS'));
  const detail = node('p', '', `${info.day} ${info.time} · ${info.period.replace(/_/g, ' ')} · Indicative order completion: ~${info.indicative_order_minutes.toFixed(1)} min (slowest parallel shipment).`);
  card.append(detail);
  const list = node('div', 'history-warehouse-list');
  for (const entry of info.warehouses) {
    const row = node('div', 'history-warehouse-row');
    const basis = entry.basis === 'matched_period' ? 'matching period' : 'overall fallback';
    row.append(node('span', '', `${entry.warehouse_id} · ${basis} (n=${entry.sample_size})`), node('strong', '', `${entry.average_minutes.toFixed(1)} min`));
    list.append(row);
  }
  card.append(list);
  if (info.changed_core_selection) card.append(node('p', 'history-highlight', 'Historical data changed the warehouse combination among options with exactly equal balanced scores.'));
  else if (info.tie_breaker_compared) card.append(node('p', '', 'Historical duration was compared among equal-score candidates.'));
  card.append(node('p', 'history-caveat', info.note + ' This is a descriptive approximation, not a delivery-time guarantee.'));
  return card;
}
function showRecommendation(rec) {
  elements.result.replaceChildren();
  setStatus('FULFILLABLE', 'success');
  const body = node('div', 'recommend-body');
  const metrics = node('div', 'result-metrics');
  metrics.append(makeMetric('Warehouses', rec.warehouse_count), makeMetric('One-way distance', rec.total_distance_km, 'km'), makeMetric('Balanced score', rec.score_km_equivalent));
  body.append(metrics);
  const title = node('div', 'result-subtitle');
  title.append(node('strong', '', 'Selected warehouses'), node('span', '', `${rec.product_split_count} SKU split${rec.product_split_count === 1 ? '' : 's'}`));
  body.append(title);
  for (const warehouse of rec.warehouses) {
    const card = node('div', 'warehouse-card');
    const head = node('div', 'warehouse-title');
    const names = node('div');
    names.append(node('strong', '', `${warehouse.warehouse_id} · ${warehouse.city}`), node('br'), node('small', '', 'Selected fulfillment source'));
    head.append(names, node('span', 'distance-pill', `${warehouse.distance_km} km`));
    card.append(head);
    const items = node('div', 'allocation-list');
    for (const item of warehouse.items) {
      const line = node('div', 'allocation-line');
      line.append(node('span', '', productName(item.product_id)), node('strong', '', `${item.quantity.toLocaleString()} units`));
      items.append(line);
    }
    card.append(items);
    body.append(card);
  }
  const breakdown = node('div', 'score-breakdown');
  breakdown.append(node('span', '', `Distance ${rec.total_distance_km} km + dispatch penalty ${rec.penalty_km_equivalent} km-eq = `), node('strong', '', `${rec.score_km_equivalent} km-eq`));
  body.append(breakdown);
  const explanation = node('div', 'decision-note');
  explanation.append(node('strong', '', 'WHY THIS COMBINATION?'), node('p', '', rec.explanation));
  body.append(explanation);
  const historyBox = showHistoricalContext(rec.history);
  if (historyBox) body.append(historyBox);
  elements.result.append(body);
}
function showShortage(data) {
  elements.result.replaceChildren();
  setStatus('INSUFFICIENT STOCK', 'warning');
  const panel = node('div', 'shortage-note');
  const hasVaccines = (data.shortages || []).some((item) => catalog.products.find((p) => p.id === item.product_id)?.requires_cold_chain);
  const policy = 'All-or-nothing policy: no partial allocation is generated.';
  const coldNote = hasVaccines ? ' Vaccine stock in warehouses without eligible refrigerated transportation cannot be used.' : '';
  panel.append(node('h4', '', 'Order cannot be fully fulfilled'), node('p', '', policy + coldNote));
  elements.result.append(panel);
  for (const shortage of data.shortages || []) {
    const card = node('div', 'shortage-row');
    card.append(node('strong', '', productName(shortage.product_id)));
    const metrics = node('div', 'shortage-numbers');
    for (const [label, value] of [['Requested', shortage.requested_quantity], ['Eligible', shortage.eligible_stock], ['Shortage', shortage.shortage_quantity]]) {
      const cell = node('span', '', label);
      cell.append(node('b', '', value.toLocaleString()));
      metrics.append(cell);
    }
    card.append(metrics);
    const note = node('p', 'physical-stock-note', `Physical stock: ${shortage.physical_stock.toLocaleString()} units across all warehouses.`);
    card.append(note);
    elements.result.append(card);
  }
}
function showError(message) {
  elements.result.replaceChildren();
  setStatus('ERROR', 'error');
  const box = node('div', 'error-state');
  box.append(node('h4', '', 'Could not process the order'), node('p', '', message));
  elements.result.append(box);
}
async function submitOrder(event) {
  event.preventDefault();
  if (isLoading) return;
  const items = [];
  for (const row of elements.rows.children) {
    const quantity = Number(row.querySelector('input').value);
    if (!Number.isSafeInteger(quantity) || quantity <= 0) {
      showError('Enter a positive whole-number quantity for every product.');
      return;
    }
    items.push({ product_id: row.querySelector('select').value, quantity });
  }
  const scheduledDay = elements.day.value;
  const scheduledTime = elements.time.value;
  if (scheduledDay && !/^([01]\d|2[0-3]):[0-5]\d$/.test(scheduledTime)) {
    showError('Enter a valid 24-hour dispatch time.');
    return;
  }
  setLoading(true);
  try {
    const body = { items };
    if (scheduledDay) body.delivery_schedule = { day: scheduledDay, time: scheduledTime };
    const response = await fetch('/api/v1/recommendations', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    const payload = await response.json();
    if (!response.ok) {
      const detail = payload.error?.details?.[0];
      throw new Error(detail ? `${detail.field}: ${detail.message}` : (payload.error?.message || 'The service returned an error.'));
    }
    if (payload.status === 'FULFILLABLE' && payload.recommendation) showRecommendation(payload.recommendation);
    else if (payload.status === 'INSUFFICIENT_ELIGIBLE_STOCK') showShortage(payload);
    else showError('The server returned an unexpected result.');
  } catch (error) {
    showError(error.message || 'Network unavailable. Please retry.');
  } finally {
    setLoading(false);
  }
}
async function loadInsights() {
  try {
    const response = await fetch('/api/v1/insights');
    if (!response.ok) throw new Error('Insight data unavailable');
    const report = await response.json();
    $('#history-count').textContent = `${report.total_records} records`;
    elements.history.replaceChildren();
    const max = Math.max(...report.warehouses.map((m) => m.average_minutes), 1);
    for (const row of report.warehouses) {
      const cell = node('div', 'bar-cell');
      const head = node('div', 'bar-row-head');
      const left = node('strong', '', row.warehouse_id);
      left.append(node('em', '', ` · n=${row.deliveries}`));
      head.append(left, node('span', '', `${row.average_minutes.toFixed(1)} min`));
      const track = node('div', 'bar-track');
      const fill = node('div', 'bar-fill');
      fill.style.width = `${(row.average_minutes / max) * 100}%`;
      track.append(fill);
      cell.append(head, track);
      const periods = node('div', 'period-summary');
      for (const p of row.periods || []) {
        const name = ({weekday_peak:'Peak',weekday_offpeak:'Weekday other',weekend:'Weekend'})[p.period] || p.period;
        periods.append(node('span', 'period-chip', `${name}: ${p.average_minutes.toFixed(1)}m · n=${p.deliveries}`));
      }
      cell.append(periods);
      elements.history.append(cell);
    }
  } catch (error) {
    elements.history.replaceChildren(node('p', 'muted', 'Historical insights could not be loaded.'));
  }
}
async function initialize() {
  try {
    const response = await fetch('/api/v1/catalog');
    if (!response.ok) throw new Error('Catalog endpoint is unavailable');
    catalog = await response.json();
    if (!catalog.products?.length || !catalog.warehouses?.length) throw new Error('Catalog is empty');
    $('#warehouse-stat').textContent = catalog.warehouses.length;
    $('#product-stat').textContent = catalog.products.length;
    $('#penalty-stat').textContent = catalog.policy?.penalty_km_equivalent ?? 20;
    $('#penalty-formula').textContent = catalog.policy?.penalty_km_equivalent ?? 20;
    addItem('P002', 45);
    $('#add-item').addEventListener('click', () => { addItem('P001', 1); showEmpty(); });
    elements.day.addEventListener('change', () => {
      elements.time.disabled = !elements.day.value;
      showEmpty();
    });
    elements.time.addEventListener('change', showEmpty);
    elements.form.addEventListener('submit', submitOrder);
    elements.rows.addEventListener('input', showEmpty);
    elements.rows.addEventListener('change', showEmpty);
    document.querySelectorAll('[data-preset]').forEach((button) => button.addEventListener('click', () => setPreset(button.dataset.preset)));
  } catch (error) {
    elements.button.disabled = true;
    $('#add-item').disabled = true;
    showError(error.message || 'Unable to initialize the application');
  }
  loadInsights();
}
initialize();
