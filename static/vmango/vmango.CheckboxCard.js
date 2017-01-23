(function(exports){
    exports.Vmango = exports.Vmango || {};
    exports.Vmango.CheckboxCards = function(selector){
        var multiple = $(selector).attr('data-CheckboxCards-Multiple') === "true",
            $cardEls = $('.JS-CheckboxCard', selector),
            defaultColor = $cardEls.first().css('border-color'),
            $checkboxEls = $cardEls.find('input[type=checkbox]');

        $checkboxEls.hide();

        $cardEls.each(function(idx, el){
            var $el = $(el),
                $icon = $el.find('.icon');

            if ($el.find('input[type=checkbox]').prop('checked')){
                $el.css({'border-color': 'green'})
                $icon.show();
            }
        });

        $checkboxEls.on('change', function($event){
            var $el = $(this),
                $cardEl = $el.closest('.JS-CheckboxCard'),
                $icon = $cardEl.find('.icon');

            if ($el.prop('checked')){
                $cardEl.css({'border-color': 'green'});
                $icon.show();
            } else {
                $cardEl.css({'border-color': defaultColor});
                $icon.hide();
            }
        });

        $cardEls.on('click', function($event){
            var newState, $allChecked,
                $el = $(this),
                $checkboxEl = $el.find('input[type=checkbox]');

            $event.preventDefault();
            newState = !$checkboxEl.prop("checked")
            if (!multiple){
                $allChecked = $cardEls.find('input[type=checkbox]:checked');
                $allChecked.prop('checked', false);
                $allChecked.trigger('change');

            }
            $checkboxEl.prop("checked", newState);
            $checkboxEl.trigger('change');
        });
    }
})(window);
