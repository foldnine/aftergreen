# AfterGreen brand guidelines

Approved visual direction: **a forensic instrument for developers**. Precise Swiss/editorial layout, warm ivory surfaces and deep graphite panels, not AI-generated-looking SaaS decoration.

## Mark

The two **separated, skewed signal planes** are canonical. Upper lime plane = reported workflow success; lower coral plane = independently observed outcome. The deliberate gap is essential. Do not merge, rotate or round the planes, redraw as a checkmark/shield, or substitute generic AI imagery.

Canonical path data:

```svg
<path d="M18 60 L34 19 L100 5 L87 45 Z" fill="#B5F36C"/><path d="M13 111 L26 74 L91 61 L77 99 Z" fill="#F27964"/>
```

## Color

| Token | Hex | Role |
| --- | --- | --- |
| Charcoal | `#101416` | Primary ink/dark surface |
| Ivory | `#F5F4EF` | Paper/light surface |
| Signal Green | `#B5F36C` | Reported workflow green |
| Incident Coral | `#F27964` | Contradictory result/failure |
| Slate | `#899397` | Secondary detail |

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

All logo SVGs are based on the approved two-plane artwork. The wordmark uses Inter text with fallback fonts, **not outlined type**; rendering may vary if Inter is unavailable. The social-preview file is vector; export to PNG for GitHub social-preview settings as a separate build step if required.
