import 'package:flutter/widgets.dart';

class TierBanner extends StatelessWidget {
  const TierBanner({super.key, required this.page});

  final PageBundle page;

  @override
  Widget build(BuildContext context) {
    final tiers = page.assignableTiers;
    return Text('${tiers.length} tiers');
  }
}
