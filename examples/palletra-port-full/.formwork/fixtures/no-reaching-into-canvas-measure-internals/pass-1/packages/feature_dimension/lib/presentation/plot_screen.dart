// feature_dimension is the sanctioned heavy consumer; except.paths carves it out,
// so it may import the measure internals directly.
import 'package:plt_canvas_kit/measure/step_state.dart';

class PlotScreen {
  final PlotNavState state;
  PlotScreen(this.state);
}
