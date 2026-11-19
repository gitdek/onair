# Pieces the writer made

The opening segment and the six pieces of one recording, as On Air's writer wrote them on a Mac, kept so that tests on any platform read the writer's own output: `init.mp4` and `piece-000001.mp4` to `piece-000006.mp4`, under the names the spool gives them.

They come from `onair selftest -keep`, run from the app built at 31952c1. The self-test records nothing, so nothing here was captured. It writes ten seconds of a made-up picture, a red bar on a dark background, in H.264 at 1280×720 and 15 frames a second, and made-up sound in AAC at 48 kHz in stereo: the display's 440 Hz tone, louder on the left, mixed as capture mixes it with the microphone's 880 Hz tone, which is quiet from 4 to 5 seconds and switches from 44.1 to 48 kHz at 7 seconds. It cuts pieces every 2 seconds, where a recording cuts them every 4, so the last piece is short.

To make them again, on a Mac: `tools/build-app`, then `"build/On Air.app/Contents/MacOS/onair" selftest -keep`, and copy `init.mp4` and the pieces from the `spool/selftest` directory it names.
