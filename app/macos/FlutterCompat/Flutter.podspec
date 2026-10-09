# Shared Darwin plugin podspecs include an iOS Flutter dependency. This local
# metadata placeholder prevents CocoaPods resolving unrelated trunk releases.
# The macOS engine is still provided and linked by FlutterMacOS / Flutter tooling.
Pod::Spec.new do |s|
  s.name = 'Flutter'
  s.version = '1.0.0'
  s.summary = 'Local metadata placeholder for shared Darwin plugin resolution.'
  s.homepage = 'https://flutter.dev'
  s.license = { :type => 'BSD' }
  s.author = { 'AgentDeck' => 'dev@agentdeck.local' }
  s.source = { :git => 'https://github.com/flutter/engine', :tag => s.version.to_s }
  s.vendored_frameworks = 'path/to/nothing'
  s.osx.deployment_target = '10.15'
end
