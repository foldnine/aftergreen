# AfterGreen brand guidelines

Approved visual direction: **a forensic instrument for developers**. Precise Swiss/editorial layout, warm ivory surfaces and deep graphite panels, not AI-generated-looking SaaS decoration.

## Mark

The two **separated, skewed signal planes** are canonical. Upper lime plane = the status a workflow *reports*; lower coral plane = the outcome AfterGreen *independently observes*. Coral marks the observed evidence, not failure: it is the plane that can contradict green. The deliberate gap is essential. Do not merge, rotate or round the planes, redraw as a checkmark/shield, or substitute generic AI imagery. Decorative use (for example a large cropped mark on the social preview) still uses the exact canonical paths, scaled uniformly.

Canonical path data:

```svg
<path d="M18 60 L34 19 L100 5 L87 45 Z" fill="#B5F36C"/><path d="M13 111 L26 74 L91 61 L77 99 Z" fill="#F27964"/>
```

## Color

| Token | Hex | Role |
| --- | --- | --- |
| Charcoal | `#101416` | Primary ink/dark surface |
| Ivory | `#F5F4EF` | Paper/light surface |
| Signal Green | `#B5F36C` | Reported workflow status |
| Incident Coral | `#F27964` | Independently observed outcome |
| Slate | `#899397` | Secondary detail |

### Contrast

- Charcoal on ivory and ivory on charcoal: body text.
- Slate is text-safe only on charcoal (5.9:1). On ivory (2.9:1) use it for rules and decoration only.
- Never set Signal Green as text on light surfaces (1.2:1). On charcoal it is fine.

Use clear textual verdicts, never color alone, for VERIFIED / FAILED / UNKNOWN. Avoid gradients, glows, robots, brains, shields or decorative dashboards that imply nonexistent features.

## Type

Inter for headlines/body; JetBrains Mono for CLI, outputs and evidence. Fall back to installed system sans and monospace. Do not commit or redistribute commercial fonts.

## Voice and messaging

- **Green isn't proof.**
- Verify what actually happened.
- Outcome evidence, not a successful workflow status.

## Important product constraint

Previous brand board mockups include **concept-only UI**. AfterGreen currently provides a Go CLI and a GitHub Action preview; no hosted UI or n8n integration has shipped. Never present exploratory mockups as real product screens.

## Source assets

All logo SVGs use the approved two-plane artwork. The wordmark in the lockups and the social preview is **outlined** (Inter ExtraBold, kerned, converted to paths), so it renders identically without Inter installed. The social preview's secondary lines are SVG text; `assets/github-social-preview.png` (1280 × 640) is the rendered export to upload under repository Settings → Social preview.
