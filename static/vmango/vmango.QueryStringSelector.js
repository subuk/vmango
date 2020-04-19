(function(exports){
    exports.Vmango = exports.Vmango || {};
    exports.Vmango.QueryStringSelector = function(selector){
        $(".JS-QueryStringSelector").on('change', function(event){
            var destUrl = $(event.target).data('url'),
                name = $(event.target).data('paramname'),
                exclusive = $(event.target).data('exclusive'),
                value = $(event.target).children("option:selected").val(),
                urlParams;

            if (exclusive) {
                urlParams = new URLSearchParams("");
            } else {
                urlParams = new URLSearchParams(window.location.search);
            }
            urlParams.set(name, value);

            exports.location.href = destUrl + "?" + urlParams.toString();
        })
    }
})(window);
