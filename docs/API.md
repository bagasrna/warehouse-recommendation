# HTTP API Contract

All payloads are UTF-8 JSON. The server includes a single responsive web frontend at `/`.

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/` | Order simulator frontend |
| `GET` | `/api/v1/catalog` | Read-only product and warehouse catalog plus selection policy |
| `POST` | `/api/v1/recommendations` | All-or-nothing order recommendation |
| `GET` | `/api/v1/insights` | Optional descriptive historical delivery statistics |
| `GET` | `/healthz` | Health check (`{"status":"ok"}`) |

## Recommendation request

```json
{
  "items": [
    {"product_id":"P002","quantity":45}
  ]
}
```

The optional `delivery_schedule` is an object with `day` (`Mon`..`Sun`) and `time` (`HH:MM`, 24-hour WIB). Both fields are required when a schedule object is supplied. For example:

```json
{"items":[{"product_id":"P002","quantity":45}],"delivery_schedule":{"day":"Mon","time":"08:00"}}
```

If omitted, the engine uses its original ranking and returns no `history` field.

Each quantity must be a positive JSON integer; product IDs must exist in the catalog. Repeated product IDs are combined before the selection process. Unknown JSON fields and malformed JSON are rejected. Request body limit is 64 KiB.

## Success (`200 OK`)

```json
{
  "status": "FULFILLABLE",
  "recommendation": {
    "warehouses": [
      {"warehouse_id":"W04","city":"Gresik","distance_km":30,"items":[{"product_id":"P002","quantity":40}]},
      {"warehouse_id":"W07","city":"Pasuruan","distance_km":45,"items":[{"product_id":"P002","quantity":5}]}
    ],
    "warehouse_count": 2,
    "total_distance_km": 75,
    "penalty_km_equivalent": 20,
    "score_km_equivalent": 95,
    "product_split_count": 1,
    "explanation": "..."
  }
}
```

When `delivery_schedule` is present, a fulfilled `recommendation` also contains `history`, for example:

```json
{
  "day":"Mon",
  "time":"08:00",
  "period":"weekday_peak",
  "indicative_order_minutes":77.5,
  "warehouses":[
    {"warehouse_id":"W04","average_minutes":77.5,"sample_size":2,"basis":"matched_period","reliable_for_tie":true},
    {"warehouse_id":"W07","average_minutes":67.5,"sample_size":2,"basis":"all_history_fallback","reliable_for_tie":false}
  ],
  "tie_breaker_compared":false,
  "changed_core_selection":false,
  "note":"..."
}
```

The values above are illustrative of field structure; the actual period data can differ (W07 has no matching weekday-peak records, and its all-history fallback cannot rank candidates). For separate concurrent dispatches, `indicative_order_minutes` is the maximum source mean, NOT their sum. This is not a guaranteed ETA or real-time traffic prediction. If all chosen sources do not have sufficient matching records, `tie_breaker_compared` remains false unless an eligible equal-score alternative was actually compared.

The `score_km_equivalent` is an optimization score, **not** real shipping distance, monetary cost, or ETA. `total_distance_km` sums the static one-way distances for selected warehouses.

## Insufficient eligible inventory (`200 OK`)

```json
{
  "status": "INSUFFICIENT_ELIGIBLE_STOCK",
  "recommendation": null,
  "shortages": [
    {
      "product_id":"P002",
      "requested_quantity":109,
      "eligible_stock":108,
      "physical_stock":158,
      "shortage_quantity":1
    }
  ],
  "explanation":"At least one item exceeds available eligible stock. No partial allocation is recommended."
}
```

If multiple products are short, each one is included in `shortages` (sorted by product ID). An insufficient response does not contain partial allocations.

## Errors

| Status | Code | Cause |
| --- | --- | --- |
| 400 | `BAD_REQUEST` | Malformed JSON, wrong JSON type, extra document, unknown field |
| 413 | `PAYLOAD_TOO_LARGE` | Request larger than 64 KiB |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | Missing/wrong Content-Type |
| 422 | `INVALID_ORDER` | Empty items, unknown product, non-positive quantity, duplicate quantity overflow |
| 500 | `INTERNAL_ERROR` | Unexpected engine or invariant failure (details logged server-side) |

Sample validation error:

```json
{
  "error": {
    "code":"INVALID_ORDER",
    "message":"Order validation failed",
    "details":[{"field":"items[0].quantity","message":"must be a positive integer"}]
  }
}
```

## Catalog response

The catalog endpoint returns `products`, `warehouses` and `policy.penalty_km_equivalent`. Warehouse objects contain immutable `stock` maps and flags `has_cold_storage`/`has_refrigerated_fleet`. `requires_cold_chain` is set per product, not derived from its category.

## Historical insights

The separate `/api/v1/insights` endpoint returns `total_records`, per-warehouse counts and overall mean durations, plus `periods` (peak, off-peak, weekend) with sample sizes and means. The core balanced score never changes. Historical durations can break **exact score ties only** and only with at least 2 period observations for every source in both compared combinations. Missing/sparse period data falls back to all-history means for display only. Departure schedule is fully optional, with no change to original behavior when omitted.
