# Domain Example: Ecommerce App QA Scopes

Illustration of expected depth when scoping QA. For your app, derive equivalent scopes from the codebase.

Severity tags: `[C]` = CRITICAL, `[H]` = HIGH, `[M]` = MEDIUM, `[L]` = LOW.

## Scope: Product Browsing — Priority: HIGH
- `[H]` View product listing — products shown with images, prices, names?
- `[H]` Filter by category — list updates? URL reflects filter?
- `[H]` Sort by price/name/date — correct sorting (not string sort on numbers)?
- `[M]` Search — returns results? No results state? Special characters?
- `[M]` Pagination — page 1, page 2, last page, page beyond range
- `[H]` Product detail — clicking a product shows full details?
- `[M]` Variant display — sizes/colors shown and selectable?
- `[H]` Stock status — in stock / out of stock displayed correctly?
- `[H]` Out of stock — add-to-cart disabled?

## Scope: Cart Management — Priority: CRITICAL
- `[C]` Add to cart — cart updates? Badge count?
- `[H]` Add with variant — correct variant in cart?
- `[C]` Add same product twice — quantity increments (not duplicate line)?
- `[C]` Change quantity — line total and cart total recalculate?
- `[H]` Remove item — item disappears, totals update?
- `[H]` Exceed stock — caps or shows error?
- `[M]` Cart persistence — survives page refresh?
- `[M]` Cart with expired/deleted product — graceful handling?
- `[H]` Empty cart — message shown, checkout blocked

## Scope: Checkout Flow — Priority: CRITICAL
- `[C]` Auth guard — /checkout without login redirects, then returns
- `[H]` Address step — add new, select existing, validation on required fields
- `[M]` Address fields — country/state dropdowns populate, postcode validates
- `[H]` Shipping step — options shown, prices vary by address/weight
- `[C]` Selecting shipping — order total updates correctly
- `[H]` Payment step — methods shown, can select
- `[C]` Order summary — matches cart + shipping + tax
- `[C]` Place order success — confirmation page with order number
- `[C]` Place order failure — error shown, no double-charge, can retry
- `[M]` Back navigation — state preserved through steps
- `[M]` Mixed scenarios — single vs multiple items, mixed variants

## Scope: Authentication — Priority: CRITICAL
- `[C]` Signup — valid data creates account and logs in
- `[H]` Signup — duplicate email, weak password, empty fields all show errors
- `[C]` Login — valid credentials logs in and redirects
- `[H]` Login — wrong password shows clear error (not generic 500)
- `[M]` Login loading state — button disabled during request, re-enables on failure
- `[C]` Logout — clears session, protected pages inaccessible
- `[H]` Session persistence — refresh while logged in stays logged in
- `[C]` Protected page access — redirect to login with return URL

## Scope: User Account — Priority: HIGH
- `[H]` Profile — shows current user info
- `[H]` Profile edit — change name/email, saves correctly
- `[H]` Address book — list, add, edit, delete, set default
- `[M]` Order history — past orders with status, totals, dates
- `[M]` Order detail — line items, shipping, payment, tracking

## Scope: Admin — Priority: HIGH
- `[H]` Dashboard — real metrics (not zeros when data exists)
- `[H]` Entity list pages — data loads, pagination, search/filter
- `[H]` Create entity — form validates, saves, appears in list
- `[H]` Edit entity — loads current values, saves, list updates
- `[H]` Delete entity — confirmation, removes from list, handles cascade
- `[C]` Role-based access — non-admin users blocked from admin routes
