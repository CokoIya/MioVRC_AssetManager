Third-party scripts of the video studio (web/studio*.js), as published on npm, unchanged:

- mp4-muxer.js   mp4-muxer 5.2.2, build/mp4-muxer.js   https://github.com/Vanilagy/mp4-muxer   MIT, LICENSE-mp4-muxer.txt
- webm-muxer.js  webm-muxer 5.1.4, build/webm-muxer.js https://github.com/Vanilagy/webm-muxer  MIT, LICENSE-webm-muxer.txt
- qrcode.js      qrcode-generator 2.0.4, dist/qrcode.js https://github.com/kazuhikoarase/qrcode-generator  MIT, LICENSE-qrcode-generator.txt

They are loaded only when the studio needs them (the QR code when it opens, a muxer when a video is exported).

The community's check of a picture before it is uploaded (web/nsfw.js), unchanged:

- tf.es2017.min.js          TensorFlow.js: @tensorflow/tfjs 4.22.0, dist/tf.es2017.min.js   https://github.com/tensorflow/tfjs
                            Apache-2.0, LICENSE-tfjs.txt
                            from https://registry.npmjs.org/@tensorflow/tfjs/-/tfjs-4.22.0.tgz (sha1 6fa2d850d39579ef190d2cb8d2451524059ddab5)
                            1105894 bytes, sha256 a1f9e32fd9b373cf394905050cf0c8c66238b34925846c065db00d2a34d1c040
- nsfwjs/model.json         NSFWJS 4.4.0, the MobileNetV2 model (224x224, weights quantized to uint8): models/mobilenet_v2/
  nsfwjs/group1-shard1of1   https://github.com/infinitered/nsfwjs   MIT, LICENSE-nsfwjs.txt
                            from https://cdn.jsdelivr.net/gh/infinitered/nsfwjs@v4.4.0/models/mobilenet_v2/model.json
                            128945 bytes, sha256 11846416217e68bf1eb7b0e651bcfd305973566453c63275bbd16766ab089979
                            and https://cdn.jsdelivr.net/gh/infinitered/nsfwjs@v4.4.0/models/mobilenet_v2/group1-shard1of1
                            2619461 bytes, sha256 8e7dddbb16acacc1bf1601b1b8a761e730ff934b7f2d7771312b2f000e5f5f13
                            (the same bytes as the base64 in npm nsfwjs 4.4.0, dist/models/mobilenet_v2/group1-shard1of1.min.js)
- LICENSE-tfjs.txt          https://cdn.jsdelivr.net/gh/tensorflow/tfjs@tfjs-v4.22.0/LICENSE
                            sha256 cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30
- LICENSE-nsfwjs.txt        https://cdn.jsdelivr.net/gh/infinitered/nsfwjs@v4.4.0/LICENSE (the same as in npm nsfwjs 4.4.0)
                            sha256 2f7fe40b4f0d97020f0a0dc2214d637471f6204e565eb83e9fd414f0e5287d94

NSFWJS's own script is not used: its browser build (2.7 MB) carries a second copy of TensorFlow.js. web/nsfw.js
runs the model as NSFWJS 4.4.0's classify does (the picture as 224x224 RGB in 0..1, the five softmax outputs in
the order Drawing, Hentai, Neutral, Porn, Sexy). Both are loaded the first time a picture is checked, never at
start. .gitattributes keeps these files byte for byte as published.

The community page's QR scanner in a browser (web/social.js, 扫一扫 and 扫码登录 on a phone), unchanged:

- jsQR.js           jsQR 1.4.0, dist/jsQR.js   https://github.com/cozmo/jsQR   Apache-2.0, LICENSE-jsqr.txt
                    from https://registry.npmjs.org/jsqr/-/jsqr-1.4.0.tgz (sha1 8efb8d0a7cc6863cb6d95116b9069123ce9eb2d1)
                    256885 bytes, sha256 bc40c8a15196236b2314db0856f72ca0b49980cd5413b8c852a7349f5fee0859
- LICENSE-jsqr.txt  the package's LICENSE, sha256 c6596eb7be8581c18be736c846fb9173b69eccf6ef94c5135893ec56bd92ba08

It is loaded the first time the camera is opened where the browser has no BarcodeDetector of its own (Safari), never
at start. The QR codes the community shows use qrcode.js above.
