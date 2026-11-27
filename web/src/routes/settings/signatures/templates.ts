/**
 * Starting-point templates inserted into the signature editor. Every template
 * uses {{placeholders}} resolved at send time, with the current user's data
 * substituted live in the preview.
 */
export interface SignatureTemplate {
  id: string;
  name: string;
  description: string;
  html: string;
}

export const SIGNATURE_TEMPLATES: SignatureTemplate[] = [
  {
    id: 'professional',
    name: 'Professional',
    description: 'Two-line sign-off with name, title and contact details.',
    html: `<table cellpadding="0" cellspacing="0" border="0" style="font-family:Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;">
  <tr>
    <td style="padding-right:12px;border-right:2px solid #0f766e;">
      <strong style="font-size:15px;">{{employee_name}}</strong><br>
      <span style="color:#475569;">{{job_title}}</span>
      {{#if department}}<br><span style="color:#475569;">{{department}}</span>{{/if}}
    </td>
    <td style="padding-left:12px;">
      <div style="color:#475569;"><strong>{{company_name}}</strong></div>
      <div><a href="tel:{{phone}}" style="color:#0f766e;text-decoration:none;">{{phone}}</a></div>
      <div><a href="mailto:{{email}}" style="color:#0f766e;text-decoration:none;">{{email}}</a></div>
      <div><a href="{{company_website}}" style="color:#0f766e;text-decoration:none;">{{company_website}}</a></div>
    </td>
  </tr>
</table>`,
  },
  {
    id: 'minimal',
    name: 'Minimal',
    description: 'One line of contact information. Great for internal signatures.',
    html: `<p style="font-family:Helvetica,Arial,sans-serif;font-size:13px;color:#475569;margin:0;">
  <strong style="color:#0f172a;">{{employee_name}}</strong> · {{job_title}} · {{company_name}}
  <br>
  <a href="mailto:{{email}}" style="color:#0f766e;text-decoration:none;">{{email}}</a>
  · <a href="tel:{{phone}}" style="color:#0f766e;text-decoration:none;">{{phone}}</a>
</p>`,
  },
  {
    id: 'with-logo',
    name: 'With logo',
    description: 'Logo-led signature — best when your brand mark is recognisable.',
    html: `<table cellpadding="0" cellspacing="0" border="0" style="font-family:Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;">
  <tr>
    <td style="padding-right:16px;vertical-align:top;">
      <img src="{{company_logo}}" alt="{{company_name}}" width="80" style="display:block;max-width:80px;height:auto;">
    </td>
    <td style="vertical-align:top;">
      <strong style="font-size:15px;">{{employee_name}}</strong><br>
      <span style="color:#475569;">{{job_title}}</span><br>
      <span style="color:#475569;">{{company_name}}</span>
      <div style="margin-top:8px;">
        <a href="mailto:{{email}}" style="color:#0f766e;text-decoration:none;">{{email}}</a>
        · <a href="tel:{{phone}}" style="color:#0f766e;text-decoration:none;">{{phone}}</a>
      </div>
      <div><a href="{{company_website}}" style="color:#0f766e;text-decoration:none;">{{company_website}}</a></div>
    </td>
  </tr>
</table>`,
  },
  {
    id: 'corporate',
    name: 'Corporate',
    description: 'Full contact card with office address and LinkedIn link.',
    html: `<table cellpadding="0" cellspacing="0" border="0" style="font-family:Helvetica,Arial,sans-serif;font-size:13px;color:#0f172a;">
  <tr>
    <td style="padding-right:14px;border-right:1px solid #e2e8f0;vertical-align:top;">
      <strong style="font-size:15px;">{{employee_name}}</strong><br>
      <span style="color:#475569;">{{job_title}}</span><br>
      <span style="color:#475569;">{{department}}</span>
    </td>
    <td style="padding-left:14px;vertical-align:top;color:#475569;">
      <div><strong style="color:#0f172a;">{{company_name}}</strong></div>
      <div>{{office_address}}</div>
      <div style="margin-top:6px;">
        <a href="mailto:{{email}}" style="color:#0f766e;text-decoration:none;">{{email}}</a>
        · <a href="tel:{{phone}}" style="color:#0f766e;text-decoration:none;">{{phone}}</a>
      </div>
      <div><a href="{{company_website}}" style="color:#0f766e;text-decoration:none;">{{company_website}}</a></div>
    </td>
  </tr>
</table>`,
  },
];
