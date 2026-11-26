import 'dart:html'; // want: trace-context-interceptor-no-web-deps
import 'dart:math';

String mint() => Random.secure().nextInt(4294967295).toString();
