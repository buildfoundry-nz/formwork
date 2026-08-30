// Renders a named-tier label from its integer code. Every case magnitude
// matches the Go internal/tier.Code const it mirrors.
String tierLabel(int code) {
  switch (code) {
    case -1:
      return 'Basement';
    case 0:
      return 'Ground';
    case 900:
      return 'Mezzanine';
    case 901:
      return 'Loft';
    default:
      return 'Level $code';
  }
}
