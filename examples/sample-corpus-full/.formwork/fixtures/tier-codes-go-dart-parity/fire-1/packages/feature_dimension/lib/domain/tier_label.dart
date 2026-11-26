// Renders a named-tier label from its integer code. The mezzanine case has
// drifted to 2000 while Go still owns tier.Mezzanine = 900, so every
// mezzanine page silently renders "Level 900".
String tierLabel(int code) {
  switch (code) {
    case -1:
      return 'Basement';
    case 0:
      return 'Ground';
    case 2000:
      return 'Mezzanine';
    case 901:
      return 'Loft';
    default:
      return 'Level $code';
  }
}
