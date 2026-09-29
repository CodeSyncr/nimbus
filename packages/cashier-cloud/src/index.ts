/**
 * @codesyncr/cashier-cloud — the web SDK.
 *
 * The browser half of Cashier Cloud, alongside the iOS and Android SDKs. It
 * carries a public key and can only read: what the paywall should sell, what
 * the subscriber owns, and where to send them to pay. Granting an entitlement
 * is Cloud's job, and only after a store or Stripe confirmed the money moved.
 *
 * Receipt verification, metering and the entitlement ledger are Cloud's, and
 * run on Cloud. They are deliberately not in this package: a credential that
 * ships to a browser is not a credential.
 */

export { CashierCloud, CashierCloudError, CashierErrorCode } from './client.js'
export type { CashierCloudOptions, CustomerInfoListener, FetchLike, PurchaseOptions, Storage } from './client.js'
export {
  decodeCustomerInfo,
  decodeEntitlementInfo,
  decodeOfferings,
  decodeProduct,
  decodeSubscription,
} from './client.js'
export { hasEntitlement, statusGrants } from './types.js'
export type {
  CustomerInfo,
  Entitlement,
  EntitlementInfo,
  EntitlementSource,
  Offering,
  OfferingsResponse,
  Package,
  PeriodType,
  Product,
  ResolvedPackage,
  SubscriberEvent,
  SubscriberEventType,
  SubscriberReason,
  Subscription,
  SubscriptionStatus,
} from './types.js'
export { renderPaywall, resolvePaywallText, formatAmount, periodWord, paywallScreens, flattenBlocks, countdownRemaining, switchPackage, paywallVariant, paywallSavings, artColor, localizePaywall, matchPaywallLocale, PAYWALL_SCHEMA_VERSION, PAYWALL_CONTAINERS, PAYWALL_VARIANTS } from './paywall.js'
export type { PaywallDoc, PaywallScreen, PaywallBlock, PaywallBlockType, PaywallLook, PaywallTheme, PaywallItem, PaywallContext, PaywallPackageView, PaywallAnswer } from './paywall.js'
export { PAYWALL_ART } from './art.js'
export { verifyCashierWebhook } from './webhook.js'
export type { CashierWebhook, CashierWebhookEvent, CashierWebhookType } from './webhook.js'
