!function(global) {
  'use strict';

  var _argRegExp = RegExp("{}"),
      formatString = function formatString() {
        var argsCount = arguments.length;
        if (!argsCount){
          return false;
        }
        var formatedString = arguments[0];
        if (argsCount == 1) {
          return formatedString;
        }
        for (var i = 1; i < argsCount; i++) {
          formatedString = formatedString.replace(_argRegExp, arguments[i]);
        }
        return formatedString;
      },
      parseJSON = function parseJSON(jsonString) {
        try {
          var parsedJSON = JSON.parse(jsonString);
        } catch (e) {
          return false;
        }
        if (parsedJSON && typeof parsedJSON === 'object' && parsedJSON !== null) {
          return parsedJSON;
        }
        return false;
      };

  global.parseJSON = parseJSON;
  global.formatString = formatString;
}(this);
