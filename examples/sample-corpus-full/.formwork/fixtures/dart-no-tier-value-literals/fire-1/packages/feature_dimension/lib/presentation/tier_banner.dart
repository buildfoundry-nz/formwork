import 'package:flutter/widgets.dart';

class TierBanner extends StatelessWidget {
  const TierBanner({super.key});

  static const tiers = <int>[-1, 0, 1, 2, 3, 4, 5, 900, 901]; // want: dart-no-tier-value-literals

  @override
  Widget build(BuildContext context) => const SizedBox.shrink();
}
