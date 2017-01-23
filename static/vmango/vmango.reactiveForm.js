(function(exports){
    exports.Vmango = exports.Vmango || {};
    exports.Vmango.ReactiveForm = function(selector){
        var $form = $(selector);
        $form.on('submit', function(){
            var $button = $('button[type=submit]', selector);
            $button.prop('disabled', true);
            $button.html(
                $button.attr('data-loading') || 'Loading...'
            );
        });
    }
})(window);
