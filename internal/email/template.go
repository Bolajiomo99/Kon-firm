package email

import "html/template"

// receiptTmpl is the customer's proof of purchase.
//
// Table layout and inline styles, deliberately. Email clients are not
// browsers: Outlook renders with Word's engine, Gmail strips <style> blocks,
// and flexbox is not reliably supported anywhere. This looks like 2005 because
// email still is.
//
// text/template's HTML escaping is why this is html/template — a product name
// is customer-visible data and must not be able to inject markup into a
// receipt.
var receiptTmpl = template.Must(template.New("receipt").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width"></head>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:-apple-system,'Segoe UI',Roboto,Arial,sans-serif;">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f4f5;padding:24px 12px;">
    <tr><td align="center">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:560px;background:#ffffff;border-radius:14px;overflow:hidden;border:1px solid #e4e4e7;">

        <tr><td style="background:#18181b;padding:22px 28px;">
          <div style="color:#ffffff;font-size:18px;letter-spacing:0.12em;text-transform:uppercase;font-weight:400;">Kon-firm</div>
        </td></tr>

        <tr><td style="padding:28px 28px 8px;">
          <div style="display:inline-block;background:#dcfce7;color:#15803d;font-size:12px;font-weight:700;padding:5px 11px;border-radius:999px;">PAYMENT CONFIRMED</div>
          <h1 style="margin:14px 0 6px;font-size:22px;color:#18181b;">Thank you, {{.CustomerName}}</h1>
          <p style="margin:0;color:#52525b;font-size:14px;line-height:1.6;">
            Monnify confirmed your payment, so your order is settled and being prepared.
            Keep this email — it is your receipt.
          </p>
        </td></tr>

        <tr><td style="padding:20px 28px 0;">
          <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="font-size:14px;">
            {{range .Lines}}
            <tr>
              <td style="padding:9px 0;color:#3f3f46;border-bottom:1px solid #f4f4f5;">{{.Quantity}} × {{.Name}}</td>
              <td style="padding:9px 0;color:#18181b;text-align:right;border-bottom:1px solid #f4f4f5;white-space:nowrap;">{{.Amount}}</td>
            </tr>
            {{end}}
          </table>
        </td></tr>

        <tr><td style="padding:14px 28px 0;">
          <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="font-size:14px;">
            <tr>
              <td style="padding:4px 0;color:#71717a;">Subtotal</td>
              <td style="padding:4px 0;text-align:right;color:#3f3f46;">{{.Subtotal}}</td>
            </tr>
            {{if .Discount}}
            <tr>
              <td style="padding:4px 0;color:#71717a;">Discount{{if .VoucherCode}} ({{.VoucherCode}}){{end}}</td>
              <td style="padding:4px 0;text-align:right;color:#dc2626;">−{{.Discount}}</td>
            </tr>
            {{end}}
            <tr>
              <td style="padding:4px 0;color:#71717a;">Delivery</td>
              <td style="padding:4px 0;text-align:right;color:{{if .FreeDelivery}}#16a34a{{else}}#3f3f46{{end}};">
                {{if .FreeDelivery}}FREE{{else}}{{.Delivery}}{{end}}
              </td>
            </tr>
            <tr>
              <td style="padding:12px 0 4px;border-top:2px solid #18181b;color:#18181b;font-weight:700;font-size:16px;">Total paid</td>
              <td style="padding:12px 0 4px;border-top:2px solid #18181b;text-align:right;color:#18181b;font-weight:700;font-size:16px;">{{.Total}}</td>
            </tr>
            <tr>
              <td colspan="2" style="padding:2px 0 0;text-align:right;color:#a1a1aa;font-size:11px;">
                Includes VAT of {{.VAT}} at {{.VATRate}}
              </td>
            </tr>
          </table>
        </td></tr>

        {{if .Address}}
        <tr><td style="padding:20px 28px 0;">
          <div style="background:#fafafa;border-radius:10px;padding:14px 16px;">
            <div style="color:#71717a;font-size:11px;text-transform:uppercase;letter-spacing:0.06em;font-weight:700;">Delivering to</div>
            <div style="color:#3f3f46;font-size:14px;margin-top:4px;">{{.Address}}</div>
          </div>
        </td></tr>
        {{end}}

        <tr><td style="padding:20px 28px 0;">
          <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="font-size:12px;color:#71717a;">
            <tr><td style="padding:3px 0;">Order reference</td><td style="padding:3px 0;text-align:right;font-family:monospace;color:#3f3f46;">{{.Reference}}</td></tr>
            {{if .MonnifyRef}}<tr><td style="padding:3px 0;">Monnify reference</td><td style="padding:3px 0;text-align:right;font-family:monospace;color:#3f3f46;">{{.MonnifyRef}}</td></tr>{{end}}
            {{if .PaymentMethod}}<tr><td style="padding:3px 0;">Paid by</td><td style="padding:3px 0;text-align:right;color:#3f3f46;">{{.PaymentMethod}}</td></tr>{{end}}
          </table>
        </td></tr>

        {{if .ReceiptURL}}
        <tr><td style="padding:24px 28px;" align="center">
          <a href="{{.ReceiptURL}}" style="display:inline-block;background:#7c3aed;color:#ffffff;text-decoration:none;padding:13px 26px;border-radius:10px;font-weight:600;font-size:14px;">View your order online</a>
        </td></tr>
        {{end}}

        <tr><td style="padding:18px 28px 24px;border-top:1px solid #f4f4f5;">
          <p style="margin:0;color:#a1a1aa;font-size:11px;line-height:1.6;">
            © {{.Year}} Kon-firm · Lagos, Nigeria<br>
            Payments secured by Monnify. Prices include VAT at {{.VATRate}}.<br>
            This receipt was sent because a payment was confirmed for this address.
          </p>
        </td></tr>

      </table>
    </td></tr>
  </table>
</body>
</html>`))
