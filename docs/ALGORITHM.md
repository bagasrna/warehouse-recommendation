# Warehouse Selection — Engineering Decisions

## Hard constraints before optimization

1. Validate the order. Merge duplicate SKUs, reject invalid/unknown items.
2. Check eligible physical inventory **per SKU**: if `requires_cold_chain` is true, a warehouse must have both cold storage and refrigerated fleet.
3. Require 100% coverage of **every** order item. If any SKU is short, return the shortage details with no partial recommendation.
4. Never mutate the catalog stock during recommendation.

## Enumerating candidate warehouses

For `W` warehouses, iterate non-empty bitmasks `1 .. (1<<W)-1`. The provided dataset has 8 warehouses => 255 candidate subsets. A subset is feasible iff, for every requested product, total **eligible** stock within the subset covers the quantity. Under this challenge's assumptions there are no shared vehicle loading limits; per-product feasibility is sufficient.

A feasible candidate's primary score is:

```
score_km_equivalent = sum(warehouse.distance_km)
                      + penalty_km_equivalent * (warehouse_count - 1)
```

The standard penalty is 20. It is a **modeling assumption**, not empirically measured delivery cost. You can override it for sensitivity experiments using the environment variable `DISPATCH_PENALTY_KM_EQ=20` (accepted range 0..10000).

Candidates are ranked by:

1. Minimum balanced score (never modified by history).
2. **With optional departure schedule only**, compare period-matched mean delivery minutes for tied-score candidates **when each warehouse in both options has at least 2 samples in the matching period**. For parallel dispatch, compare the slowest warehouse mean (`max(...)`). If evidence is insufficient, skip this tie-break.
3. Minimum warehouse count.
4. Minimum SKU split count (sum of `max(0, sources_for_sku - 1)`).
5. Lexicographically ascending warehouse IDs.

This allows 3 warehouses to beat 2 warehouses **only when balanced scores tie and there is sufficient matching historical evidence**. A lower balanced score always wins regardless of recorded durations.

## Product allocation

Within each feasible subset, for each SKU enumerate eligible source subsets. Minimize the number of source warehouses, then source distance sum, then IDs. Fill sources from nearest to farthest, in deterministic order. This avoids splitting a SKU when one source warehouse could provide its full quantity.

The allocation is verified before replying: each order quantity must be covered exactly; every individual allocation must be positive, stock-bounded, and eligible. An unexpected algorithm invariant failure is an internal error (500), not an insufficient-stock business result.

## Examples

- P001 x250 => W03, score 25.
- P001 x400 => W01+W03, score 50.
- P002 x45 => W04+W07, score 95 (30+45 distance, 20 penalty).
- P002 x49 => W08, score 150 (W04+W07 together only have 48 eligible vials).
- P002 x109 => insufficient; 108 eligible/158 physical, shortage 1.
- P001 x600 => W01+W05, score 115; tied with another 3-warehouse candidate, so the fewer-warehouse tie-breaker wins.

## Complexity & boundaries

Exhaustive enumeration is exponential in warehouse count. The implementation explicitly caps the warehouse catalog at 16 entries; the provided 8-warehouse case is fast. This PoC is not a general vehicle routing or warehouse inventory reservation system.

## Historical sample

38 delivery records are grouped by warehouse and departure period:

- `weekday_peak`: Mon-Fri 07:00-09:59 and 16:00-18:59
- `weekday_offpeak`: all other weekday times
- `weekend`: Saturday and Sunday (pooled due to sparse data)

The optional `delivery_schedule` includes a weekday and departure clock time in WIB. For each selected warehouse, the API returns an indicative mean and sample count for its matching period (`n >= 2`) or an **overall historical fallback** (shown for context only, not used for tie-breaking). The order-level indicative time is the maximum warehouse mean under the simplifying assumption that separate warehouse dispatches depart in parallel.

For the time-aware tie-break, the **normal score winner remains the fallback**. Only alternatives with the exact same score are considered, and only if *both the core winner and that alternative* have at least 2 observations in the matching period for every warehouse. This design avoids spurious route changes based on single deliveries. It provides exploratory evidence, not a production-quality ETA. Real-time traffic, warehouse handling time, and route synchronization are not modeled.

The source CSV's legacy Notes placeholders and line endings are not used by the analysis. No ML, cache, database, or real-time service is required.

## Assumptions & omissions

- One customer location; distances are static and one-way.
- Each warehouse dispatches independently.
- No third-party refrigerated fleet fallback.
- No authentication, mutable inventory, persistence, order placement, fleet capacity, traffic service, or ETA guarantee.
- Tests and benchmarks verify correctness and local runtime, but **deployed service uptime remains a deployment responsibility**.
